import type { GenerationStage } from "./types";

export const GENERATION_STAGE_ORDER: GenerationStage[] = ["collecting", "scripting", "finishing"];

export const GENERATING_STATUS_MESSAGES: Record<GenerationStage, string> = {
  collecting: "ニュースを集めています",
  scripting: "内容を読みやすく整理しています",
  finishing: "番組を準備しています",
};
