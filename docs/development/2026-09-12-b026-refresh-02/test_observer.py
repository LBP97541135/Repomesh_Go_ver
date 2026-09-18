import importlib.util
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("refresh_observer", Path(__file__).with_name("observer.py"))
OBSERVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(OBSERVER)


class ParseTimeTest(unittest.TestCase):
    def test_accepts_zero_through_six_fraction_digits_with_z_and_offset(self):
        for width in range(7):
            fraction = "" if width == 0 else "." + "123456"[:width]
            microsecond = 0 if width == 0 else int("123456"[:width].ljust(6, "0"))
            for suffix, offset in (("Z", timezone.utc), ("+05:30", timezone(timedelta(hours=5, minutes=30)))):
                with self.subTest(width=width, suffix=suffix):
                    actual = OBSERVER.parse_time(f"2026-09-12T12:34:56{fraction}{suffix}")
                    expected = datetime(2026, 9, 12, 12, 34, 56, microsecond, offset).astimezone(timezone.utc)
                    self.assertEqual(actual, expected)

    def test_rejects_timestamp_without_timezone(self):
        with self.assertRaises(OBSERVER.ControlledFailure):
            OBSERVER.parse_time("2026-09-12T12:34:56.1")


if __name__ == "__main__":
    unittest.main()
