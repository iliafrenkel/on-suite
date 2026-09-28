#!/usr/bin/env python3
"""Tally the Tuesday-night table tennis league from the raw scoresheet."""

from collections import defaultdict

# Each entry is (winner, loser, sets_won, sets_lost) for one match.
RESULTS = [
    ("Priya", "Tom", 3, 1),
    ("Tom", "Wei", 3, 2),
    ("Priya", "Wei", 3, 0),
    ("Dev", "Priya", 3, 2),
    ("Wei", "Dev", 3, 1),
    ("Tom", "Dev", 3, 0),
]

wins = defaultdict(int)
sets_for = defaultdict(int)
sets_against = defaultdict(int)

for winner, loser, won, lost in RESULTS:
    wins[winner] += 1
    sets_for[winner] += won
    sets_against[winner] += lost
    sets_for[loser] += lost
    sets_against[loser] += won

players = set(wins) | set(sets_for)
standings = sorted(
    players, key=lambda p: (wins[p], sets_for[p] - sets_against[p]), reverse=True
)

print(f"{'Player':<8} {'Wins':>4} {'Set diff':>9}")
for p in standings:
    diff = sets_for[p] - sets_against[p]
    print(f"{p:<8} {wins[p]:>4} {diff:>+9}")
