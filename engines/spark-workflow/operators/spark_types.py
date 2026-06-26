"""Spark type aliases used by operator metadata imports."""

from typing import Any

try:
    from pyspark.sql import DataFrame, SparkSession
except ModuleNotFoundError:
    DataFrame = Any
    SparkSession = Any
