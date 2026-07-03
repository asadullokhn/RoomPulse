import unittest

from txtuner import TX_LEVELS, parse_show

SHOW = """RoomPulse nRF52840 validation beacon
uuid  11111111-2222-3333-4444-555555555555
major 1  minor 101
tx 8 dBm  adv 300 ms  measured-power -75
"""


class ParseShowTest(unittest.TestCase):
    def test_parses_full_output(self):
        self.assertEqual(parse_show(SHOW), {
            "major": 1, "minor": 101, "tx": 8, "adv": 300,
            "uuid": "11111111-2222-3333-4444-555555555555",
        })

    def test_negative_tx(self):
        self.assertEqual(parse_show(SHOW.replace("tx 8 dBm", "tx -16 dBm"))["tx"], -16)

    def test_garbage_returns_none(self):
        self.assertIsNone(parse_show("lorem ipsum"))

    def test_level_set(self):
        self.assertEqual(TX_LEVELS, [-40, -20, -16, -12, -8, -4, 0, 2, 3, 4, 5, 6, 7, 8])


if __name__ == "__main__":
    unittest.main()
