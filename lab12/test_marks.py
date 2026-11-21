import time
import unittest
from pathlib import Path

from selenium import webdriver
from selenium.webdriver.common.by import By

PAGE = Path(__file__).with_name("marks.html").resolve().as_uri()


class MarksTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = webdriver.Chrome()

    @classmethod
    def tearDownClass(cls):
        cls.driver.quit()

    def check(self, value, expected):
        self.driver.get(PAGE)
        self.driver.find_element(By.ID, "mark").send_keys(value)
        self.driver.find_element(By.ID, "check").click()
        time.sleep(1)  # so the result is visible in the video
        actual = self.driver.find_element(By.ID, "result").text
        print(f"\ninput={value}  expected={expected}  actual={actual}")
        self.assertEqual(actual, expected)

    def test_1_minus_one(self):
        self.check("-1", "Rejected")

    def test_2_zero(self):
        self.check("0", "Accepted")

    def test_3_five(self):
        self.check("5", "Accepted")

    def test_4_ten(self):
        self.check("10", "Accepted")

    def test_5_eleven(self):
        self.check("11", "Rejected")


if __name__ == "__main__":
    unittest.main(verbosity=2)