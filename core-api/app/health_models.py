from enum import StrEnum
from typing import Literal

from pydantic import BaseModel


class ServiceStatus(StrEnum):
    READY = "ready"
    DEGRADED = "degraded"
    UNAVAILABLE = "unavailable"


class DependencyState(StrEnum):
    OK = "ok"
    UNAVAILABLE = "unavailable"


class HealthChecks(BaseModel):
    postgres: DependencyState
    redis: DependencyState


class HealthReport(BaseModel):
    status: ServiceStatus
    checks: HealthChecks


class ReadyReport(HealthReport):
    status: Literal[ServiceStatus.READY]


class LiveReport(BaseModel):
    status: Literal["alive"]


class ReadinessError(BaseModel):
    error: Literal["dependency_unavailable"]
    message: str
