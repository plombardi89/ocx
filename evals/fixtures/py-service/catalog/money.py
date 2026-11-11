"""Amounts of money as integer cents."""

import re

_AMOUNT = re.compile(r"(\d+)(?:\.(\d{1,2}))?")


def parse_dollars(text: str) -> int:
    """Parse a non-negative dollar amount such as "12", "12.5", or "12.34" into cents.

    Raises ValueError for signs, spaces, or more than two decimal places.
    """
    match = _AMOUNT.fullmatch(text)
    if match is None:
        raise ValueError(f"invalid amount: {text!r}")
    whole, frac = match.groups()
    cents = int((frac or "0").ljust(2, "0"))
    return int(whole) * 100 + cents


def format_cents(cents: int) -> str:
    """Format cents as dollars, for example "$12.34" or "-$0.05"."""
    sign = "-" if cents < 0 else ""
    dollars, rest = divmod(abs(cents), 100)
    return f"{sign}${dollars}.{rest:02d}"
