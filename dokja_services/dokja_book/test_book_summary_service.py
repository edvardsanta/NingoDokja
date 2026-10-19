import unittest
from unittest.mock import patch

from book_summary_service import BookSummaryService
from summary_utils import extractive_overview


class FakeClassifier:
    def predict_format(self, payload):
        filename = str(payload.get("filename") or "")
        if filename.endswith(".epub"):
            return "epub"
        return "pdf"

    def predict_book_type(self, payload):
        text = " ".join([str(payload.get("title") or ""), str(payload.get("content") or "")]).lower()
        if "programming" in text or "api" in text:
            return "technical"
        return "fiction"


class BookSummaryServiceTest(unittest.TestCase):
    def test_extractive_overview_prefers_repeated_relevant_sentences(self):
        result = extractive_overview(
            [
                "Intro short note",
                "Distributed systems need consistency, fault tolerance, and explicit tradeoffs across boundaries",
                "APIs need consistency because consistency reduces operational surprises in distributed systems",
                "A random decorative sentence about the weather and colors in the sky",
                "Fault tolerance and consistency shape the system design more than decorative details",
            ]
        )

        self.assertIn("consistency", result.lower())
        self.assertIn("fault tolerance", result.lower())

    def test_classify_uses_zero_shot_labels(self):
        service = BookSummaryService(classifier=FakeClassifier())

        result = service.classify(
            {
                "title": "Programming APIs",
                "filename": "guide.epub",
                "content": "This programming guide explains API design and data models.",
            }
        )

        self.assertEqual(result["format"], "epub")
        self.assertEqual(result["book_type"], "technical")

    def test_summarize_keeps_classification_result(self):
        service = BookSummaryService(classifier=FakeClassifier())

        result = service.summarize(
            {
                "title": "Programming APIs",
                "filename": "guide.pdf",
                "content": "Programming is about tradeoffs. APIs need consistency.\n\nExamples matter for system design.",
            }
        )

        self.assertEqual(result["classification"]["format"], "pdf")
        self.assertEqual(result["classification"]["book_type"], "technical")
        self.assertTrue(result["summary"]["overview"])

    def test_summarize_uses_extracted_content_when_missing(self):
        service = BookSummaryService(classifier=FakeClassifier())
        with patch(
            "book_summary_service.extract_resource_content",
            return_value="Chapter one. Systems matter.\n\nExamples clarify architecture.",
        ):
            result = service.summarize(
                {
                    "title": "Programming APIs",
                    "filename": "guide.pdf",
                }
            )

        self.assertEqual(result["classification"]["format"], "pdf")
        self.assertTrue(result["summary"]["overview"])

    def test_summarize_uses_injected_overview_generator(self):
        class FakeOverviewGenerator:
            def generate(self, content, text_sentences):
                return "custom overview"

        service = BookSummaryService(
            classifier=FakeClassifier(),
            overview_generator=FakeOverviewGenerator(),
        )

        result = service.summarize(
            {
                "title": "Programming APIs",
                "filename": "guide.pdf",
                "content": "Programming is about tradeoffs. APIs need consistency.\n\nExamples matter for system design.",
            }
        )

        self.assertEqual(result["summary"]["overview"], "custom overview")

    def test_baseline_response_shapes_kept_by_the_book_facade(self):
        # Step 03 baseline (steps.json, ADR 0006): the fields a book.* facade must keep.
        service = BookSummaryService(classifier=FakeClassifier())
        payload = {
            "title": "Programming APIs",
            "filename": "guide.pdf",
            "content": "Programming is about tradeoffs. APIs need consistency.\n\nExamples matter.",
        }

        classified = service.classify(payload)
        summarized = service.summarize(payload)

        self.assertEqual(
            set(classified),
            {
                "status",
                "format",
                "book_type",
                "language",
                "goal",
                "resource_strategy",
                "context_strategy",
                "agent_route",
            },
        )
        self.assertEqual((classified["language"], classified["goal"]), ("pt-BR", "study"))
        self.assertEqual(set(summarized), {"status", "title", "classification", "summary"})
        self.assertEqual(
            set(summarized["summary"]),
            {"overview", "key_points", "recommended_focus", "compression"},
        )
        self.assertEqual(
            set(summarized["summary"]["compression"]),
            {"input_chars", "normalized_chars", "paragraphs", "sentences"},
        )

    def test_baseline_summarize_without_content_or_resource_is_refused(self):
        service = BookSummaryService(classifier=FakeClassifier())
        with patch("book_summary_service.extract_resource_content", return_value=""):
            with self.assertRaises(ValueError):
                service.summarize({"title": "Nothing here"})


if __name__ == "__main__":
    unittest.main()
