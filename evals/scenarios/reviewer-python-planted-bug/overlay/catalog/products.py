"""The product catalog."""

from dataclasses import dataclass, field


@dataclass
class Product:
    """An item for sale. Prices are integer cents."""

    id: str
    name: str
    price_cents: int
    stock: int = 0
    tags: list[str] = field(default_factory=list)


class Catalog:
    """An in-memory catalog of products, keyed by ID."""

    def __init__(self) -> None:
        self._products: dict[str, Product] = {}

    def add(
        self,
        product_id: str,
        name: str,
        price_cents: int,
        stock: int = 0,
        tags: list[str] = [],
    ) -> Product:
        """Add a product, replacing any product with the same ID.

        The product gets the given tags plus "new". Raises ValueError for an
        empty ID or name, or a negative price or stock.
        """
        if not product_id:
            raise ValueError("product ID must not be empty")
        if not name:
            raise ValueError(f"product {product_id!r} needs a name")
        if price_cents < 0:
            raise ValueError(f"product {product_id!r} has a negative price: {price_cents}")
        if stock < 0:
            raise ValueError(f"product {product_id!r} has negative stock: {stock}")
        tags.append("new")
        product = Product(product_id, name, price_cents, stock, tags)
        self._products[product_id] = product
        return product

    def mark_seen(self, product_id: str) -> None:
        """Remove the "new" tag from a product. Raises KeyError for an unknown ID."""
        self.get(product_id).tags.remove("new")

    def get(self, product_id: str) -> Product:
        """Return the product with the given ID. Raises KeyError if there is none."""
        try:
            return self._products[product_id]
        except KeyError:
            raise KeyError(f"no product with ID {product_id!r}") from None

    def all(self) -> list[Product]:
        """Return every product, sorted by ID."""
        return sorted(self._products.values(), key=lambda p: p.id)
