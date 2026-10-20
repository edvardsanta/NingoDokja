"""Structural validation only; classification policy belongs to the Knowledge domain."""

import json
from datetime import date

FORMS = {"book", "article", "note", "conversation", "meme", "paper", "list", "song", "report",
         "letter", "manual", "liturgy", "law"}
KINDS = {"news", "reference", "research", "narrative"}
ORIGINS = {"user", "classifier", "source_default", "user_required"}
FIELDS = {"form", "content_kind", "form_choice", "kind_choice", "topics", "entities",
          "date_anchor", "reading", "classifier"}


def encode_metadata(value):
    """Canonical JSON for an explicit metadata object, including {} to clear it."""
    if not isinstance(value, dict) or set(value) - FIELDS:
        raise ValueError("metadata must be an object with supported fields")
    for key, allowed in (("form", FORMS), ("content_kind", KINDS)):
        if key in value and value[key] is not None and (not isinstance(value[key], str) or value[key] not in allowed):
            raise ValueError(f"metadata {key} is unsupported")
    for key in ("form_choice", "kind_choice"):
        if key not in value:
            continue
        choice = value[key]
        if not isinstance(choice, dict) or set(choice) - {
            "origin", "classifier_id", "classifier_version", "fallback_reason"
        }:
            raise ValueError("metadata classification provenance is invalid")
        if not isinstance(choice.get("origin"), str) or choice["origin"] not in ORIGINS:
            raise ValueError("metadata classification origin is invalid")
        if any(not isinstance(v, str) for v in choice.values()):
            raise ValueError("metadata classification provenance must contain strings")
        if choice["origin"] == "classifier" and not all(
            choice.get(k, "").strip() for k in ("classifier_id", "classifier_version")
        ):
            raise ValueError("metadata classifier identity and version are required")
    for axis, key in (("form", "form_choice"), ("content_kind", "kind_choice")):
        if axis in value and key in value:
            pending = value[key]["origin"] == "user_required"
            if pending != (value[axis] is None):
                raise ValueError("metadata axis and classification provenance disagree")
    if "topics" in value and (not isinstance(value["topics"], list) or any(
        not isinstance(t, str) or not t.strip() for t in value["topics"]
    )):
        raise ValueError("metadata topics must be nonempty strings")
    if "entities" in value:
        if not isinstance(value["entities"], list):
            raise ValueError("metadata entities must be a list")
        for entity in value["entities"]:
            if (not isinstance(entity, dict) or set(entity) - {"name", "type"}
                    or not isinstance(entity.get("name"), str) or not entity["name"].strip()
                    or ("type" in entity and not isinstance(entity["type"], str))):
                raise ValueError("metadata entities require a name and optional type")
    if "date_anchor" in value:
        anchor = value["date_anchor"]
        try:
            valid = isinstance(anchor, str) and date.fromisoformat(anchor).isoformat() == anchor
        except ValueError:
            valid = False
        if not valid:
            raise ValueError("metadata date_anchor must be a valid YYYY-MM-DD date")
    if "reading" in value:
        reading = value["reading"]
        if (not isinstance(reading, dict) or set(reading) != {"format", "reader", "reader_version"}
                or any(not isinstance(v, str) for v in reading.values())):
            raise ValueError("metadata reading provenance is invalid")
    if "classifier" in value:
        classifier = value["classifier"]
        if (not isinstance(classifier, dict) or set(classifier) != {"id", "version"}
                or any(not isinstance(v, str) or not v.strip() for v in classifier.values())):
            raise ValueError("metadata classifier provenance is invalid")
    encoded = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    if len(encoded.encode()) > 65536:
        raise ValueError("metadata exceeds 65536 bytes")
    return encoded
