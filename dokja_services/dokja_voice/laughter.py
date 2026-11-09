"""Laughter in a transcript, written the way the group writes it."""
import re

LAUGHTER = "kkkk"

# Only laugh syllables, at least two of them: "hehehe", "ha ha", "ahahah", "kkk", "rsrs", "huehue".
_SYLLABLES = re.compile(r"^(?:k{3,}|(?:rs){2,}|(?:hue){2,}|(?:h?[aeiu]h?){2,})$")
# A sound tag some models write instead: "*laughs*", "(risos)", "[risadas]".
_TAG = re.compile(r"^[*(\[]\s*(?:laugh(?:s|ing|ter)?|risos?|risadas?|rindo)\s*[*)\]]$", re.IGNORECASE)


def is_laughter(text: str) -> bool:
    """Whether a transcript segment is laughter rather than words."""
    stripped = text.strip()
    if _TAG.match(stripped):
        return True
    letters = re.sub(r"[\s\W_]+", "", stripped.lower())
    # "E aí", "Ah!" or "é" are words; a laugh repeats a syllable.
    if len(letters) < 3 or len(set(letters)) > 3 or not _SYLLABLES.match(letters):
        return False
    # A drawn-out vowel ("aaa") is not a laugh; vowel laughs carry the h ("haha", "ahah").
    return letters.startswith(("k", "rs", "hue")) or "h" in letters


def with_laughter(text: str) -> str:
    """The segment as kkkk when it is laughter, otherwise unchanged."""
    return LAUGHTER if is_laughter(text) else text
