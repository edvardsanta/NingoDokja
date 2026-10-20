import type { MemeItem } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { usePreview } from "./use_preview.js";

// The memes waiting in the queue, as a grid to attach one from when writing a message. A meme is
// not a workspace of its own: it is something Ningo sends, so it is picked where messages are
// written. A click attaches a meme, a click on the attached one takes it off again.
export function MemePicker({
  transport,
  memes,
  selected,
  disabled,
  onSelect,
}: {
  transport: Transport;
  memes: readonly MemeItem[];
  selected: string;
  disabled: boolean;
  onSelect: (url: string) => void;
}) {
  const t = useTranslate();
  // a meme with no address cannot be attached
  const attachable = memes.filter((meme) => meme.url !== "");

  if (attachable.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("memes_empty")}
      </p>
    );
  }

  return (
    <ul className="memes" aria-label={t("communications_meme")}>
      {attachable.map((meme) => (
        <li key={meme.url}>
          <MemeTile
            transport={transport}
            meme={meme}
            attached={selected === meme.url}
            disabled={disabled}
            onToggle={() => onSelect(selected === meme.url ? "" : meme.url)}
          />
          <p className="meme-title">{meme.title || t("memes_untitled")}</p>
        </li>
      ))}
    </ul>
  );
}

// A meme is a picture or a short video; the shell says which by the type of the data it hands back.
// In the grid a clip is a silent loop, like a GIF.
function Media({ dataUrl, label }: { dataUrl: string; label: string }) {
  if (!dataUrl.startsWith("data:video/")) return <img src={dataUrl} alt="" />;
  return <video src={dataUrl} aria-label={label} autoPlay muted loop playsInline preload="auto" disablePictureInPicture />;
}

function MemeTile({
  transport,
  meme,
  attached,
  disabled,
  onToggle,
}: {
  transport: Transport;
  meme: MemeItem;
  attached: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, true);
  const title = meme.title || t("memes_untitled");
  return (
    <button
      type="button"
      className="preview preview-open"
      aria-label={t("memes_attach", { title })}
      aria-pressed={attached}
      aria-busy={preview.phase === "loading"}
      disabled={disabled}
      onClick={onToggle}
    >
      {preview.phase === "ready" && <Media dataUrl={preview.dataUrl} label={title} />}
      {preview.phase === "failed" && <span>{t("memes_no_preview")}</span>}
    </button>
  );
}
