# Items service notes

These notes were written during design review and never turned into a reference.

To list items, clients send a GET to /v1/items; creating one is a POST to the
same path. A single item is read with GET /v1/items/{id}, replaced with PUT,
and removed with DELETE on that path. Health is exposed at GET /healthz, which
is not versioned.

The old /v0/items endpoint was removed in March and must not be documented.
Pagination uses a `cursor` query parameter on the list call.
