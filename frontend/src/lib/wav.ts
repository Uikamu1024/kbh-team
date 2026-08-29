// バックエンドはチャプターごとに別々のWAVファイルを配信する
// （docs/api-contract.yaml参照）。プレイヤーで<audio>のsrcを差し替えながら
// 連続再生しようとすると、チャプターが変わるたびにplay()を呼び直す必要があり
// ブラウザの自動再生制限に引っかかって止まってしまう。
// これを避けるため、取得した全チャプターのWAVをクライアント側で1本のWAVに
// 結合し、単一の<audio>要素で最初の1回だけplay()すれば最後まで連続再生できる
// ようにする。

interface WavFormat {
  audioFormat: number;
  channels: number;
  sampleRate: number;
  byteRate: number;
  blockAlign: number;
  bitsPerSample: number;
}

interface ParsedWav {
  format: WavFormat;
  data: Uint8Array;
}

function parseWav(buffer: ArrayBuffer): ParsedWav {
  const view = new DataView(buffer);
  const readTag = (offset: number) =>
    String.fromCharCode(view.getUint8(offset), view.getUint8(offset + 1), view.getUint8(offset + 2), view.getUint8(offset + 3));

  if (buffer.byteLength < 12 || readTag(0) !== "RIFF" || readTag(8) !== "WAVE") {
    throw new Error("invalid RIFF/WAVE header");
  }

  let format: WavFormat | null = null;
  let data: Uint8Array | null = null;
  let offset = 12;
  while (offset + 8 <= buffer.byteLength) {
    const tag = readTag(offset);
    const chunkSize = view.getUint32(offset + 4, true);
    const bodyStart = offset + 8;
    if (bodyStart + chunkSize > buffer.byteLength) {
      throw new Error("WAV chunk exceeds input");
    }

    if (tag === "fmt ") {
      format = {
        audioFormat: view.getUint16(bodyStart, true),
        channels: view.getUint16(bodyStart + 2, true),
        sampleRate: view.getUint32(bodyStart + 4, true),
        byteRate: view.getUint32(bodyStart + 8, true),
        blockAlign: view.getUint16(bodyStart + 12, true),
        bitsPerSample: view.getUint16(bodyStart + 14, true),
      };
    } else if (tag === "data") {
      data = new Uint8Array(buffer, bodyStart, chunkSize);
    }

    offset = bodyStart + chunkSize + (chunkSize % 2);
  }

  if (!format || !data) {
    throw new Error("WAV is missing fmt or data chunk");
  }
  return { format, data };
}

function sameFormat(a: WavFormat, b: WavFormat): boolean {
  return (
    a.audioFormat === b.audioFormat &&
    a.channels === b.channels &&
    a.sampleRate === b.sampleRate &&
    a.byteRate === b.byteRate &&
    a.blockAlign === b.blockAlign &&
    a.bitsPerSample === b.bitsPerSample
  );
}

function buildWavHeader(format: WavFormat, dataLength: number): ArrayBuffer {
  const header = new ArrayBuffer(44);
  const view = new DataView(header);
  const writeTag = (offset: number, tag: string) => {
    for (let i = 0; i < 4; i++) view.setUint8(offset + i, tag.charCodeAt(i));
  };

  writeTag(0, "RIFF");
  view.setUint32(4, 36 + dataLength, true);
  writeTag(8, "WAVE");
  writeTag(12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, format.audioFormat, true);
  view.setUint16(22, format.channels, true);
  view.setUint32(24, format.sampleRate, true);
  view.setUint32(28, format.byteRate, true);
  view.setUint16(32, format.blockAlign, true);
  view.setUint16(34, format.bitsPerSample, true);
  writeTag(36, "data");
  view.setUint32(40, dataLength, true);
  return header;
}

export interface ConcatenatedAudio {
  blob: Blob;
  /** 各チャプターの実際の再生時間（秒）。結合後の音声データから算出した正確な値 */
  chapterDurations: number[];
}

/** 複数のWAV ArrayBufferを1本のWAV Blobに結合する。フォーマットが一致しない
 * ものが混ざっていた場合はエラーを投げる（このアプリでは全チャプターが同じ
 * TTSパイプラインから生成されるため通常は発生しない）。 */
export function concatenateWavBuffers(buffers: ArrayBuffer[]): ConcatenatedAudio {
  if (buffers.length === 0) {
    throw new Error("no audio buffers to concatenate");
  }

  const parsed = buffers.map(parseWav);
  const format = parsed[0].format;
  for (const p of parsed) {
    if (!sameFormat(format, p.format)) {
      throw new Error("chapter audio formats do not match");
    }
  }

  const totalLength = parsed.reduce((sum, p) => sum + p.data.length, 0);
  const combined = new Uint8Array(totalLength);
  let offset = 0;
  const chapterDurations: number[] = [];
  for (const p of parsed) {
    combined.set(p.data, offset);
    offset += p.data.length;
    chapterDurations.push(p.data.length / format.byteRate);
  }

  const header = buildWavHeader(format, combined.length);
  const blob = new Blob([header, combined], { type: "audio/wav" });
  return { blob, chapterDurations };
}
