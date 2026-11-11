# catalog

A small in-memory product catalog in Python.

## Commands

- Test: `make test` (runs `python3 -m unittest discover -s tests -v`)
- Syntax check: `python3 -m py_compile catalog/*.py`

No third-party packages are needed; tests use the standard library's `unittest`.

## Layout

- `catalog/money.py`: amounts as integer cents
- `catalog/products.py`: the `Catalog` class and `Product` records
- `tests/`: unit tests, one file per module
- `docs/plans/`: plans for multi-step work

## Conventions

- Money is always integer cents; never use floats for amounts.
- Public functions have type hints and docstrings.
- Errors are raised as `ValueError` or `KeyError` with a message that names the bad value.
