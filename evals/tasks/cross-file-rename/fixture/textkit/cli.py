import sys

from textkit.core import norm_ws


def main() -> None:
    for line in sys.stdin:
        print(norm_ws(line))


if __name__ == "__main__":
    main()
