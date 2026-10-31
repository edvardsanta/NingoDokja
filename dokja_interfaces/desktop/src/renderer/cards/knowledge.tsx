import { useContext, useState } from "react";

import type { KnowledgeStatus } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, OpenSystem } from "./card_frame.js";
import { KnowledgeDocumentsPanel } from "./knowledge_documents.js";
import { LibraryContext, StandaloneLibrary, useLibrary } from "./library_context.js";
import { SearchForm, SearchResults, SearchState, useKnowledgeSearch } from "./knowledge_search.js";

// The Documents view of the Library: finding in it, and the list. What outlives a read of the status
// (the open documents, the drawer) lives above, in the workspace's library; a view used alone brings
// its own.
export function KnowledgeCard({ transport }: { transport: Transport }) {
  const outer = useContext(LibraryContext);
  return outer ? <KnowledgeView /> : (
    <StandaloneLibrary transport={transport}>
      <KnowledgeView />
    </StandaloneLibrary>
  );
}

function KnowledgeView() {
  const t = useTranslate();
  const library = useLibrary();

  // The panel's refresh reads the counts quietly: what the owner has open stays where it is.
  return (
    <CardFrame
      title={t("knowledge_title")}
      quietTitle
      fix={<OpenSystem />}
      state={library.status}
      onReload={library.reload}
      onRefresh={library.poll}
      refreshing={library.refreshing}
    >
      {(status) => (status.off ? <><OffNotice reason={status.reason} /><OpenSystem /></> : <KnowledgeBody status={status} />)}
    </CardFrame>
  );
}

function KnowledgeBody({ status }: { status: KnowledgeStatus }) {
  const { transport, open, close, changed, revision } = useLibrary();
  const search = useKnowledgeSearch(transport);
  // An empty Library has nothing to search and nothing to count: the list says so alone. That is
  // known once the list has answered, so a Library that may be empty waits for it.
  const [total, setTotal] = useState<number>();
  const bare = status.documents === 0 && (total === undefined || total === 0);

  return (
    <>
      {!bare && (
        <div className="library-tools">
          <SearchForm search={search} />
        </div>
      )}
      {!bare && <SearchState search={search} />}
      <KnowledgeDocumentsPanel
        transport={transport}
        revision={revision}
        onChanged={changed}
        onOpen={open}
        onRemoved={close}
        status={status}
        onTotal={setTotal}
        results={search.active && !bare ? <SearchResults search={search} onOpen={(hit) => open({ id: hit.sourceId, title: hit.title })} /> : undefined}
      />
    </>
  );
}
