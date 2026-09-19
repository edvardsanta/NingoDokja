from chunker import chunk_text


def test_a_short_document_is_one_chunk():
    chunks = chunk_text("Uma ideia curta sobre estoicismo.")
    assert [(c.position, c.heading, c.text) for c in chunks] == [(0, "", "Uma ideia curta sobre estoicismo.")]


def test_empty_or_blank_bodies_have_no_chunks():
    assert chunk_text("") == []
    assert chunk_text("   \n\n  \n") == []
    assert chunk_text("# So um titulo\n") == []


def test_chunks_carry_their_heading_path_and_never_cross_a_heading():
    body = "# Etica\nAbertura da etica.\n\n## Virtude\nA virtude e um habito.\n\n# Logica\nRegras de inferencia."
    chunks = chunk_text(body)
    assert [(c.heading, c.text) for c in chunks] == [
        ("Etica", "Abertura da etica."),
        ("Etica > Virtude", "A virtude e um habito."),
        ("Logica", "Regras de inferencia."),
    ]


def test_a_long_document_is_split_under_the_limit_with_a_shared_tail():
    sentence = "Esta e uma frase de teste com varias palavras para encher espaco."
    body = "\n\n".join(f"{sentence} Numero {i}." for i in range(40))
    chunks = chunk_text(body, max_chars=400, overlap=80)

    assert len(chunks) > 3
    assert all(len(c.text) <= 400 + 80 for c in chunks)
    assert [c.position for c in chunks] == list(range(len(chunks)))
    # The tail of one chunk reappears at the start of the next, so a cut sentence survives.
    first_tail = chunks[0].text.split()[-3:]
    assert " ".join(first_tail) in chunks[1].text


def test_a_single_huge_paragraph_is_split_by_sentence_and_then_by_word():
    long_sentence = "palavra " * 500
    chunks = chunk_text(f"Primeira frase curta. {long_sentence}", max_chars=300, overlap=0)
    assert len(chunks) >= 3
    assert all(len(c.text) <= 300 for c in chunks)


def test_windows_line_endings_are_handled():
    chunks = chunk_text("# T\r\nlinha um\r\nlinha dois\r\n\r\noutro paragrafo\r\n")
    assert chunks[0].heading == "T"
    assert "linha um linha dois" in chunks[0].text
