"""Request and response models for the decision engine's HTTP surface.

The two required shapes are ``PosthocTrainRequest`` and ``DecisionRequest``;
everything else is metadata that makes the service debuggable from Foundry
without a log dive.
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

DecisionType = Literal["boolean", "enum", "numeric"]

#: Scenario names become blob path segments, so they are restricted to a safe
#: alphabet rather than sanitised silently.
ScenarioName = Annotated[
    str,
    Field(
        min_length=1,
        max_length=128,
        pattern=r"^[A-Za-z0-9][A-Za-z0-9_.-]*$",
        examples=["loan-approval"],
    ),
]


class TrainSample(BaseModel):
    """One labelled example.

    ``label`` carries the ground truth for the scenario's decision type:
    ``0``/``1`` (or ``false``/``true``) for boolean, a class index or class name
    for enum, and the target value for numeric.
    """

    model_config = ConfigDict(extra="forbid")

    input: dict[str, Any]
    label: bool | int | float | str

    @field_validator("input")
    @classmethod
    def _input_not_empty(cls, value: dict[str, Any]) -> dict[str, Any]:
        if not value:
            raise ValueError("input must not be empty")
        return value


class PosthocTrainRequest(BaseModel):
    """Body of ``POST /posthoc_train``."""

    model_config = ConfigDict(extra="forbid")

    decision_type: DecisionType
    scenario: ScenarioName
    training_data: list[TrainSample] = Field(min_length=2)
    class_names: list[str] | None = Field(
        default=None,
        description=(
            "Enum only: ordered class names. Index i names class i, and string "
            "labels in training_data are resolved against this list. When "
            "omitted, string labels define the ordering (sorted, first seen)."
        ),
    )
    validation_split: float | None = Field(
        default=None,
        ge=0.0,
        lt=1.0,
        description="Overrides JEV_VALIDATION_SPLIT for this fit.",
    )
    numeric_bins: int | None = Field(
        default=None, ge=2, description="Numeric only: overrides JEV_NUMERIC_BINS."
    )
    numeric_tolerance: float | None = Field(
        default=None,
        gt=0.0,
        description="Numeric only: overrides JEV_NUMERIC_TOLERANCE.",
    )
    notes: str | None = Field(
        default=None, max_length=1024, description="Free text stored with the artifact."
    )

    @model_validator(mode="after")
    def _check_type_specific(self) -> PosthocTrainRequest:
        if self.decision_type != "enum" and self.class_names:
            raise ValueError("class_names is only meaningful for enum scenarios")
        if self.class_names is not None:
            if len(self.class_names) < 2:
                raise ValueError("class_names needs at least two entries")
            if len(set(self.class_names)) != len(self.class_names):
                raise ValueError("class_names must be unique")
        if self.decision_type != "numeric" and (
            self.numeric_bins is not None or self.numeric_tolerance is not None
        ):
            raise ValueError(
                "numeric_bins/numeric_tolerance are only meaningful for numeric scenarios"
            )
        return self


class FitMetrics(BaseModel):
    """What the fit achieved, on the holdout when there is one."""

    model_config = ConfigDict(extra="forbid")

    split: Literal["validation", "train"]
    num_samples: int
    negative_log_likelihood: float | None = None
    brier_score: float | None = None
    expected_calibration_error: float | None = None
    accuracy: float | None = None
    mean_absolute_error: float | None = None
    coverage: float | None = None


class PosthocTrainResponse(BaseModel):
    """Body of a successful ``POST /posthoc_train``."""

    model_config = ConfigDict(extra="forbid")

    status: Literal["ok"] = "ok"
    scenario: str
    decision_type: DecisionType
    num_classes: int | None = None
    class_names: list[str] | None = None
    num_samples: int
    num_train_samples: int
    num_validation_samples: int
    temperature: float
    temperature_clamped: bool = Field(
        default=False,
        description=(
            "True when the fit landed on JEV_TEMPERATURE_MIN/_MAX rather than "
            "an interior optimum. That usually means the labels carry little "
            "signal the base model can see: the optimiser kept flattening (or "
            "sharpening) the distribution until it ran out of room. Treat the "
            "calibration as suspect and look at the metrics."
        ),
    )
    calibration_version: str
    artifacts: list[str]
    metrics_before: FitMetrics | None = None
    metrics_after: FitMetrics | None = None
    duration_ms: float


class DecisionRequest(BaseModel):
    """Body of ``POST /decision``.

    ``data`` is the caller's payload. Either pass the model input directly
    (``{"text": "..."}``) or wrap it (``{"input": {"text": "..."}}``); both are
    accepted so Foundry tool schemas can use whichever shape reads better.
    """

    model_config = ConfigDict(extra="forbid")

    decision_type: DecisionType
    scenario: ScenarioName
    data: dict[str, Any] = Field(min_length=1)
    threshold: float | None = Field(
        default=None,
        ge=0.0,
        le=1.0,
        description="Boolean only: overrides JEV_BOOLEAN_THRESHOLD for this call.",
    )
    include_probabilities: bool = Field(
        default=False,
        description="Enum only: also return the full calibrated distribution.",
    )

    def model_input(self) -> dict[str, Any]:
        """Return the dict the base model should score."""
        nested = self.data.get("input")
        if isinstance(nested, dict) and nested:
            return nested
        return self.data


class DecisionResponse(BaseModel):
    """Body of a successful ``POST /decision``."""

    model_config = ConfigDict(extra="forbid")

    value: bool | int | float
    probability: float = Field(ge=0.0, le=1.0)
    scenario: str
    decision_type: DecisionType
    label: str | None = Field(
        default=None, description="Enum only: the class name for `value`."
    )
    probabilities: dict[str, float] | None = Field(
        default=None, description="Enum only, when include_probabilities is set."
    )
    calibrated: bool = Field(
        description="False when the raw model output was returned uncalibrated."
    )
    calibration_version: str | None = None
    latency_ms: float


class ScenarioInfo(BaseModel):
    """One row of ``GET /scenarios``."""

    model_config = ConfigDict(extra="forbid")

    scenario: str
    decision_type: DecisionType
    calibration_version: str
    num_classes: int | None = None
    class_names: list[str] | None = None
    num_samples: int | None = None
    trained_at: str | None = None


class ScenarioListResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scenarios: list[ScenarioInfo]


class HealthResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    status: Literal["ok", "degraded"]
    service: str
    model_backend: str
    model_name: str
    model_loaded: bool
    registry: str
    cached_scenarios: int


class ErrorResponse(BaseModel):
    """Every 4xx/5xx body the service produces."""

    model_config = ConfigDict(extra="forbid")

    error: str
    detail: str
    scenario: str | None = None
    decision_type: str | None = None
