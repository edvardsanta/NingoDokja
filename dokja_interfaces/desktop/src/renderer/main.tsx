import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@fontsource/b612-mono/latin-400.css";
import "@fontsource/b612-mono/latin-700.css";
import "@fontsource/bodoni-moda/latin-700-italic.css";

import { App } from "./app.js";
import { loadSession } from "./session.js";
import "./styles.css";

const container = document.getElementById("root");
if (!container) throw new Error("the page has no #root element");

// The fonts are local and small. Waiting for them (briefly) avoids a flash of the fallback face.
const fontsReady = Promise.race([
  Promise.all([
    document.fonts.load('400 1em "B612 Mono"'),
    document.fonts.load('700 1em "B612 Mono"'),
    document.fonts.load('italic 700 1em "Bodoni Moda"'),
  ]),
  new Promise((resolve) => setTimeout(resolve, 1500)),
]);

void Promise.all([loadSession(), fontsReady]).then(([{ transport, locale }]) => {
  createRoot(container).render(
    <StrictMode>
      <App transport={transport} locale={locale} />
    </StrictMode>,
  );
});
