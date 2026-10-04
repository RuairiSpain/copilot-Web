"""Validation engine.

Phase 1 (:func:`validate_declared`) checks the configuration as authored. Phase 2
(:func:`validate_effective`) checks the normalised configuration after inheritance and
implicit resources. Each diagnostic code ``XF0nn`` is the number of a rule in the
specification's "Required semantic validation outside JSON Schema" list.
"""

from xfoundry.validators.declared import validate_declared
from xfoundry.validators.effective import validate_effective

__all__ = ["validate_declared", "validate_effective"]
