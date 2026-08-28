import { MAX_TAGS, PRESET_TAGS } from "@/lib/presetTags";

interface TagPickerProps {
  selected: string[];
  onChange: (tags: string[]) => void;
}

export function TagPicker({ selected, onChange }: TagPickerProps) {
  function toggle(tag: string) {
    if (selected.includes(tag)) {
      onChange(selected.filter((t) => t !== tag));
    } else if (selected.length < MAX_TAGS) {
      onChange([...selected, tag]);
    }
  }

  return (
    <div className="flex flex-wrap gap-2">
      {PRESET_TAGS.map((tag) => {
        const isSelected = selected.includes(tag);
        const atLimit = selected.length >= MAX_TAGS && !isSelected;
        return (
          <button
            key={tag}
            type="button"
            disabled={atLimit}
            onClick={() => toggle(tag)}
            className={`rounded-full border px-4 py-2 text-[13px] font-semibold transition-all ${
              isSelected
                ? "border-accent bg-accent text-[#06120a]"
                : "border-bg-elevated-3 bg-transparent text-text-secondary hover:border-text-secondary hover:text-text-primary"
            } ${atLimit ? "cursor-not-allowed opacity-35" : "cursor-pointer"}`}
          >
            {tag}
          </button>
        );
      })}
    </div>
  );
}
