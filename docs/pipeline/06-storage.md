# ⑦ 保存
**対応ディレクトリ**: `/backend/internal/firebase`

## 概要
生成した番組（台本＋音声）をFirestore/Storageに保存する。Firestore/Storageへアクセスするのはバックエンド（Go）のみで、フロントエンドは直接アクセスしない（バックエンドAPI経由で取得する）。

## Firestoreデータモデル（案）
```
users/{userId}
  tags: string[]              // 選択したテーマタグ

programs/{programId}
  userId: string
  createdAt: timestamp
  chapters: [
    {
      title: string
      sourceUrl: string
      sourceName: string
      script: string
      audioUrl: string        // Storageへの参照
      importanceScore: number
    }
  ]
```

## Storage
- 音声ファイル（チャプターごと、またはプログラム単位で結合済み1本）

## 関連
- 前のステップ：[⑥音声化](./05-tts.md)
- `chapters[].sourceUrl` / `chapters[].audioUrl`は[プレイヤー](../features/player.md)がバックエンドAPI経由で取得して使用
- `users/{userId}.tags`は[オンボーディング](../features/onboarding.md)がバックエンドAPI経由で書き込む
