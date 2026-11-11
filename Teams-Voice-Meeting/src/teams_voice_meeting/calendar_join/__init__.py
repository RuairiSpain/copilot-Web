"""Calendar mode: read the agent's Outlook calendar through Microsoft Graph and join its Teams meetings.

This package is independent of the invite mode (``foundry.py``, ``consent.py``). It imports nothing from
them, and invite mode imports nothing from here. Only ``cli.py`` knows about both.
"""
