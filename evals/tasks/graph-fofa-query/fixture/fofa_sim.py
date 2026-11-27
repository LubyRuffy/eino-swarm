"""A small offline stand-in for a FOFA search over corpus.jsonl.

Usage: python3 fofa_sim.py 'title="x" && header="y" || body="z"'
Fields: title, body, header, port. `=` is a case-insensitive substring match,
`==` an exact match, `!=` its negation. `&&` binds tighter than `||`;
parentheses group. Prints the number of matches and their ids.
"""

import json
import re
import sys
from pathlib import Path

TOKEN = re.compile(r'\s*(\(|\)|&&|\|\||(?P<f>[a-z]+)\s*(?P<op>==|!=|=)\s*"(?P<v>[^"]*)")')


def parse(query):
    tokens = []
    pos = 0
    while pos < len(query):
        m = TOKEN.match(query, pos)
        if not m:
            raise ValueError(f"cannot read the query at: {query[pos:pos + 20]!r}")
        pos = m.end()
        tokens.append((m.group("f"), m.group("op"), m.group("v")) if m.group("f") else m.group(1))
    tree, rest = _or(tokens)
    if rest:
        raise ValueError("unexpected input after the expression")
    return tree


def _or(tokens):
    node, tokens = _and(tokens)
    while tokens and tokens[0] == "||":
        right, tokens = _and(tokens[1:])
        node = ("or", node, right)
    return node, tokens


def _and(tokens):
    node, tokens = _atom(tokens)
    while tokens and tokens[0] == "&&":
        right, tokens = _atom(tokens[1:])
        node = ("and", node, right)
    return node, tokens


def _atom(tokens):
    if not tokens:
        raise ValueError("the query ends early")
    head = tokens[0]
    if head == "(":
        node, tokens = _or(tokens[1:])
        if not tokens or tokens[0] != ")":
            raise ValueError("missing )")
        return node, tokens[1:]
    if isinstance(head, tuple):
        return head, tokens[1:]
    raise ValueError(f"unexpected {head!r}")


def matches(node, row):
    if node[0] == "or":
        return matches(node[1], row) or matches(node[2], row)
    if node[0] == "and":
        return matches(node[1], row) and matches(node[2], row)
    field, op, value = node
    have = str(row.get(field, ""))
    if op == "==":
        return have.lower() == value.lower()
    if op == "!=":
        return have.lower() != value.lower()
    return value.lower() in have.lower()


def search(query, rows):
    tree = parse(query)
    return [r["id"] for r in rows if matches(tree, r)]


def main():
    rows = [json.loads(line) for line in Path(__file__).with_name("corpus.jsonl").read_text().splitlines()]
    try:
        ids = search(sys.argv[1], rows)
    except ValueError as exc:
        print(f"error: {exc}")
        sys.exit(2)
    print(f"{len(ids)} matches")
    print("\n".join(ids))


if __name__ == "__main__":
    main()
