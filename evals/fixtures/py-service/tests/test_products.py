import unittest

from catalog.products import Catalog


class CatalogTest(unittest.TestCase):
    def test_add_get_all(self) -> None:
        catalog = Catalog()
        for product_id in ["c", "a", "b"]:
            catalog.add(product_id, f"item {product_id}", 100, stock=1)
        self.assertEqual(catalog.get("b").name, "item b")
        self.assertEqual([p.id for p in catalog.all()], ["a", "b", "c"])

    def test_get_missing(self) -> None:
        with self.assertRaises(KeyError):
            Catalog().get("missing")

    def test_add_validates(self) -> None:
        catalog = Catalog()
        bad = [("", "x", 1, 0), ("a", "", 1, 0), ("a", "x", -1, 0), ("a", "x", 1, -1)]
        for args in bad:
            with self.subTest(args=args):
                with self.assertRaises(ValueError):
                    catalog.add(*args)
        self.assertEqual(catalog.all(), [])


if __name__ == "__main__":
    unittest.main()
