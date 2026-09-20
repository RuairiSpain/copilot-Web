"""Environment-driven configuration for the Jev BYOM decision engine.

Everything a developer is likely to want to change between a laptop, CI and a
Container App is an environment variable with a documented default. The prefix
is ``JEV_`` for engine settings; the two blob settings also accept the
unprefixed names used by the deployment scripts (``BLOB_CONN_STR`` /
``BLOB_CONTAINER_NAME``) so the Container App env-vars block can stay as-is.

Import ``get_settings()`` rather than constructing ``Settings`` directly: it is
cached, so the process reads the environment once and every module sees the
same object.
"""

from __future__ import annotations

from functools import lru_cache
from typing import Literal

from pydantic import AliasChoices, Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

ModelBackend = Literal["huggingface", "hash"]


class Settings(BaseSettings):
    """Runtime configuration, read once from the environment."""

    model_config = SettingsConfigDict(
        env_prefix="JEV_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        # `model_name` and friends would otherwise collide with pydantic's
        # protected `model_` namespace and emit warnings.
        protected_namespaces=(),
    )

    # ------------------------------------------------------------------
    # Base model
    # ------------------------------------------------------------------
    model_backend: ModelBackend = Field(
        default="huggingface",
        description=(
            "'huggingface' loads a real sequence-classification checkpoint. "
            "'hash' is a deterministic, dependency-free stand-in used by tests "
            "and smoke deployments — it never downloads anything."
        ),
    )
    model_name: str = Field(
        default="distilbert-base-uncased",
        description=(
            "Hugging Face model id or local path of the base model. Swap in "
            "your mini-jev-DistilBERT / OpenJev-E5 checkpoint here."
        ),
    )
    model_revision: str | None = Field(
        default=None,
        description="Pin the checkpoint to a git revision/commit for reproducibility.",
    )
    model_cache_dir: str | None = Field(
        default=None, description="HF cache directory (maps to HF_HOME semantics)."
    )
    model_device: str = Field(
        default="cpu", description="torch device string: cpu, cuda, cuda:0, mps."
    )
    model_num_labels: int = Field(
        default=2,
        ge=2,
        description=(
            "Number of output logits the base model exposes. Enum scenarios may "
            "use fewer classes than this; they never use more."
        ),
    )
    model_max_length: int = Field(
        default=256, ge=8, le=8192, description="Tokenizer truncation length."
    )
    model_batch_size: int = Field(
        default=16, ge=1, description="Batch size used when scoring training data."
    )
    model_text_fields: tuple[str, ...] = Field(
        default=("text", "input", "prompt", "query", "content"),
        description=(
            "Keys tried, in order, when pulling the text out of a request's "
            "input dict. The first present, non-empty string wins."
        ),
    )
    model_eager_load: bool = Field(
        default=True,
        description=(
            "Load the checkpoint during startup rather than on the first "
            "request. Keep it on in production so a cold container fails fast "
            "instead of timing out a caller."
        ),
    )

    # ------------------------------------------------------------------
    # Calibration
    # ------------------------------------------------------------------
    temperature_init: float = Field(
        default=1.0, gt=0, description="Starting temperature for the LBFGS fit."
    )
    temperature_min: float = Field(
        default=0.05, gt=0, description="Lower clamp on the fitted temperature."
    )
    temperature_max: float = Field(
        default=20.0, gt=0, description="Upper clamp on the fitted temperature."
    )
    temperature_max_iter: int = Field(
        default=100, ge=1, description="LBFGS iteration budget."
    )
    temperature_lr: float = Field(
        default=0.05, gt=0, description="LBFGS learning rate."
    )
    isotonic_out_of_bounds: Literal["clip", "nan"] = Field(
        default="clip",
        description="How isotonic regression handles inputs outside the fitted range.",
    )
    isotonic_min_samples: int = Field(
        default=8,
        ge=2,
        description=(
            "Minimum samples per class before an isotonic regressor is fitted. "
            "Below this the class falls back to temperature-scaled softmax, "
            "which is safer than a two-point step function."
        ),
    )
    numeric_bins: int = Field(
        default=10, ge=2, description="Quantile bins used by numeric calibration."
    )
    numeric_min_samples_per_bin: int = Field(
        default=3,
        ge=1,
        description="Bins thinner than this are merged into their neighbour.",
    )
    numeric_tolerance: float = Field(
        default=0.1,
        gt=0,
        description=(
            "Half-width of the band used to define 'correct' for a numeric "
            "decision. Relative to the label range unless "
            "JEV_NUMERIC_TOLERANCE_ABSOLUTE is set."
        ),
    )
    numeric_tolerance_absolute: bool = Field(
        default=False,
        description="Treat JEV_NUMERIC_TOLERANCE as an absolute value, not a fraction.",
    )
    boolean_probability: Literal["confidence", "true"] = Field(
        default="confidence",
        description=(
            "What the `probability` field means for a boolean decision. "
            "'confidence' reports the probability of the value that was "
            "returned (so it matches the enum semantics and is never below the "
            "threshold's complement); 'true' always reports P(value is True)."
        ),
    )
    boolean_threshold: float = Field(
        default=0.5,
        ge=0.0,
        le=1.0,
        description="Calibrated probability at or above which a boolean decision is True.",
    )
    validation_split: float = Field(
        default=0.2,
        ge=0.0,
        lt=1.0,
        description=(
            "Fraction of /posthoc_train samples held out to score the fit. "
            "0 disables the holdout and reports training-set metrics only."
        ),
    )
    train_min_samples: int = Field(
        default=10, ge=2, description="Reject a /posthoc_train call below this size."
    )
    train_max_samples: int = Field(
        default=50_000, ge=2, description="Reject a /posthoc_train call above this size."
    )
    random_seed: int = Field(
        default=42, description="Seed for the train/validation split."
    )

    # ------------------------------------------------------------------
    # Blob-backed registry
    # ------------------------------------------------------------------
    blob_conn_str: str | None = Field(
        default=None,
        validation_alias=AliasChoices(
            "BLOB_CONN_STR", "BLOBCONNSTR", "JEV_BLOB_CONN_STR"
        ),
        description=(
            "Storage account connection string. Leave unset to authenticate "
            "with a managed identity via JEV_BLOB_ACCOUNT_URL instead."
        ),
    )
    blob_account_url: str | None = Field(
        default=None,
        validation_alias=AliasChoices("BLOB_ACCOUNT_URL", "JEV_BLOB_ACCOUNT_URL"),
        description="https://<account>.blob.core.windows.net — used with DefaultAzureCredential.",
    )
    blob_container_name: str = Field(
        default="jev-calibration",
        validation_alias=AliasChoices(
            "BLOB_CONTAINER_NAME", "BLOBCONTAINERNAME", "JEV_BLOB_CONTAINER_NAME"
        ),
        description="Blob container holding calibration artifacts.",
    )
    blob_prefix: str = Field(
        default="calibration",
        description="Key prefix inside the container; lets one container host several engines.",
    )
    local_registry_dir: str | None = Field(
        default=None,
        description=(
            "Filesystem directory to use instead of Blob Storage. Set for local "
            "development and tests; leave unset in Azure."
        ),
    )
    registry_cache_ttl_seconds: float = Field(
        default=300.0,
        ge=0.0,
        description="In-memory calibration cache TTL. 0 disables caching.",
    )
    registry_cache_max_entries: int = Field(
        default=256, ge=1, description="Maximum cached (scenario, decision_type) entries."
    )
    registry_create_container: bool = Field(
        default=True,
        description="Create the blob container on startup when it is missing.",
    )

    # ------------------------------------------------------------------
    # Calibration descriptions
    # ------------------------------------------------------------------
    describe_llm_url: str | None = Field(
        default=None,
        description=(
            "OpenAI-shaped chat-completions URL used to rewrite a generated "
            "calibration description (e.g. "
            "https://<resource>/openai/v1/chat/completions). Unset means "
            "descriptions are computed from the training data alone, which "
            "needs no network and cannot hallucinate."
        ),
    )
    describe_llm_model: str = Field(
        default="gpt-4o-mini", description="Model or deployment name for the rewrite."
    )
    describe_llm_token: str | None = Field(
        default=None, description="Bearer token for the describe endpoint."
    )
    describe_llm_api_key: str | None = Field(
        default=None, description="api-key header for the describe endpoint, if it wants one."
    )
    describe_llm_max_tokens: int = Field(default=200, ge=16)
    describe_llm_timeout: float = Field(default=15.0, gt=0)
    default_on_conflict: Literal["new_version", "new_scenario", "reject"] = Field(
        default="new_version",
        description=(
            "What POST /posthoc_train does when the scenario already exists. "
            "'new_version' adds a version and moves the pointer (the previous "
            "version stays readable and can be pinned); 'new_scenario' keeps "
            "the existing one untouched and trains <scenario>-2, -3, ...; "
            "'reject' returns 409."
        ),
    )

    # ------------------------------------------------------------------
    # Service
    # ------------------------------------------------------------------
    api_key: str | None = Field(
        default=None,
        description=(
            "When set, every /decision and /posthoc_train call must present it "
            "as 'x-api-key' or 'Authorization: Bearer ...'. Foundry's model "
            "connection stores the same value."
        ),
    )
    require_calibration: bool = Field(
        default=False,
        description=(
            "Reject /decision for an uncalibrated scenario instead of falling "
            "back to the raw softmax. Turn this on in production once every "
            "scenario has been trained."
        ),
    )
    log_level: str = Field(default="INFO", description="Root log level.")
    log_json: bool = Field(
        default=True, description="Emit one JSON object per log line."
    )
    service_name: str = Field(default="jev-byom", description="Name used in logs.")

    @model_validator(mode="after")
    def _check_consistency(self) -> Settings:
        if self.temperature_min >= self.temperature_max:
            raise ValueError("JEV_TEMPERATURE_MIN must be below JEV_TEMPERATURE_MAX")
        if not (self.temperature_min <= self.temperature_init <= self.temperature_max):
            raise ValueError(
                "JEV_TEMPERATURE_INIT must lie within "
                "[JEV_TEMPERATURE_MIN, JEV_TEMPERATURE_MAX]"
            )
        if self.train_min_samples > self.train_max_samples:
            raise ValueError("JEV_TRAIN_MIN_SAMPLES must not exceed JEV_TRAIN_MAX_SAMPLES")
        if (
            self.local_registry_dir is None
            and self.blob_conn_str is None
            and self.blob_account_url is None
        ):
            raise ValueError(
                "No calibration store configured: set BLOB_CONN_STR (or "
                "BLOB_ACCOUNT_URL for managed identity), or JEV_LOCAL_REGISTRY_DIR "
                "for local runs."
            )
        return self


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    """Return the process-wide settings, reading the environment on first call."""
    return Settings()


def reset_settings_cache() -> None:
    """Drop the cached settings. Tests use this after changing the environment."""
    get_settings.cache_clear()
