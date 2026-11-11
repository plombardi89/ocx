import unittest

from catalog.products import Catalog


class CatalogTest(unittest.TestCase):
    def test_add_get_all(self) -> None:
        catalog = Catalog()
        for product_id in ["c", "a", "b"]:
            catalog.add(product_id, f"item {product_id}", 100, stock=1, tags=["fruit"])
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


class TagsTest(unittest.TestCase):
    def test_new_product_is_tagged_new(self) -> None:
        catalog = Catalog()
        product = catalog.add("a", "Apple", 50)
        self.assertEqual(product.tags, ["new"])

    def test_given_tags_come_first(self) -> None:
        catalog = Catalog()
        product = catalog.add("b", "Banana", 25, tags=["fruit"])
        self.assertEqual(product.tags, ["fruit", "new"])

    def test_mark_seen_removes_new(self) -> None:
        catalog = Catalog()
        catalog.add("b", "Banana", 25, tags=["fruit"])
        catalog.mark_seen("b")
        self.assertEqual(catalog.get("b").tags, ["fruit"])

    def test_mark_seen_unknown_product(self) -> None:
        with self.assertRaises(KeyError):
            Catalog().mark_seen("missing")


if __name__ == "__main__":
    unittest.main()
