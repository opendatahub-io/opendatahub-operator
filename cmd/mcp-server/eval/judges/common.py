"""Shared helpers for judge modules."""

import re


STOP_WORDS = frozenset({
    "the", "a", "an", "is", "in", "to", "of", "and", "or", "for",
    "with", "on", "at", "by", "from", "as", "that", "this", "it",
    "be", "are", "was", "were", "been", "being", "has", "have",
    "had", "do", "does", "did", "will", "would", "could", "should",
    "not", "no", "but", "if", "then", "than", "too", "very",
})


def get_diagnosis_text(outputs):
    """Extract diagnosis text, preferring the longest file content."""
    best = ""
    for value in outputs.get("files", {}).values():
        if isinstance(value, str) and len(value) > len(best):
            best = value
    if best:
        return best
    for key in ("output_content", "main_content", "conversation", "stdout"):
        val = outputs.get(key)
        if val and isinstance(val, str) and len(val) > len(best):
            best = val
    return best


def word_match(phrase, text):
    """Word-boundary match to prevent substring collisions."""
    return bool(re.search(r"(?<!\w)" + re.escape(phrase) + r"(?!\w)", text))
