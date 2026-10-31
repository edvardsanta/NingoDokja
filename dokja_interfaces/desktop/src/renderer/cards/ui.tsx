import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";

type ButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "className" | "type"> & {
  // primary is the one main action of a view; quiet is a secondary action in a row or a footer.
  variant?: "default" | "primary" | "quiet";
  // compact is for what sits in a header, a row or a footer.
  size?: "default" | "compact";
  // Never left to the browser, which would make a button inside a form submit it.
  type?: "button" | "submit";
  // A class of the feature that places or styles this particular button.
  extra?: string;
};

// A native button with its look named, not spelled in class strings. Everything else it is given,
// such as aria attributes, disabled or handlers, reaches the element as it is.
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button({ variant = "default", size = "default", type = "button", extra, ...rest }, ref) {
  const classes = [variant === "default" ? "" : variant, size === "default" ? "" : size, extra ?? ""].filter(Boolean).join(" ");
  return <button ref={ref} type={type} className={classes || undefined} {...rest} />;
});

export type NoticeTone = "problem" | "progress" | "ok" | "off" | "unchecked";

// A line of feedback. `announce` decides what a screen reader does, and only when the text is
// something the person should hear when it changes: polite waits its turn (loading, saved, waiting),
// assertive interrupts (a failure). Streaming text must never be announced line by line. `boxed` draws
// the note with its rule; a `tone` needs it.
export function Notice({ announce, boxed = false, tone, as: Tag = "p", extra, children }: {
  announce?: "polite" | "assertive";
  boxed?: boolean;
  tone?: NoticeTone;
  as?: "p" | "div" | "span";
  extra?: string;
  children: ReactNode;
}) {
  const classes = [boxed ? "note" : "", extra ?? ""].filter(Boolean).join(" ");
  const role = announce === "assertive" ? "alert" : announce === "polite" ? "status" : undefined;
  return <Tag role={role} className={classes || undefined} data-tone={boxed ? tone : undefined}>{children}</Tag>;
}

// Ningo's line, then what to do next in plain words: one unit, the same pair as in Talk and the
// Notebook. The voice line is one sentence with no number and no button; the plain line follows it.
export function VoiceBlock({ voice, next }: { voice: string; next: string }) {
  return (
    <div className="voice-block">
      <p className="voice">{voice}</p>
      <p className="detail">{next}</p>
    </div>
  );
}
