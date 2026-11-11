# Low-stock report

The low-stock report leverages a performant, idempotent filtering paradigm to facilitate the seamless surfacing of SKU-level inventory deficits across the catalog, enabling best-in-class replenishment workflows.

How it works:

- The caller passes a threshold to `Store.LowStock`.
- The store takes a snapshot of the catalog, sorted by ID.
- Every product whose stock is below the threshold is collected.
- The collected products are returned to the caller.
