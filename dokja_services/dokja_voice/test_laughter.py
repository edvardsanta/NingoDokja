import unittest

from laughter import is_laughter, with_laughter


class LaughterTests(unittest.TestCase):
    def test_laugh_syllables_and_sound_tags_become_kkkk(self):
        # Portuguese test data: these are how Whisper writes laughter heard in pt-BR rooms.
        for text in ["Hehehehe", "Ha ha ha!", "Ah, ah, ah, ah.", "ahahahah", "kkkkk", "rsrsrs",
                     "huehuehue", "Hihihi.", "*laughs*", "(risos)", "[risadas]"]:
            with self.subTest(text=text):
                self.assertEqual(with_laughter(text), "kkkk")

    def test_words_are_never_taken_for_laughter(self):
        for text in ["E aí", "Ah!", "é", "Ei", "Oi, Ningo", "Haha, que engraçado", "aaa", "Uhum", "kk",
                     "Hahaha mas falando sério", "Ahh", "hehe sim"]:
            with self.subTest(text=text):
                self.assertFalse(is_laughter(text))
                self.assertEqual(with_laughter(text), text)


if __name__ == "__main__":
    unittest.main()
