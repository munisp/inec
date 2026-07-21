"""Export authoritative election results to an interactive GeoLibre map."""

from __future__ import annotations

import geolibre
import pandas as pd

_REQUIRED_COLUMNS = ("latitude", "longitude", "polling_unit", "voter_turnout")


def _validate_export_frame(df: pd.DataFrame) -> pd.DataFrame:
    """Return a validated copy of an authoritative election-results frame.

    The exporter must not manufacture coordinates, polling-unit identifiers, or
    turnout values. Callers therefore receive a descriptive error whenever the
    supplied data is incomplete or contains invalid geographic coordinates.
    """
    if df.empty:
        raise ValueError("GeoLibre export requires at least one authoritative election-result row")

    missing_columns = [column for column in _REQUIRED_COLUMNS if column not in df.columns]
    if missing_columns:
        raise ValueError(
            "GeoLibre export requires authoritative columns: "
            f"{', '.join(_REQUIRED_COLUMNS)}; missing: {', '.join(missing_columns)}"
        )

    export_frame = df.loc[:, _REQUIRED_COLUMNS].copy()
    export_frame["latitude"] = pd.to_numeric(export_frame["latitude"], errors="coerce")
    export_frame["longitude"] = pd.to_numeric(export_frame["longitude"], errors="coerce")

    invalid_coordinates = (
        export_frame["latitude"].isna()
        | export_frame["longitude"].isna()
        | ~export_frame["latitude"].between(-90, 90)
        | ~export_frame["longitude"].between(-180, 180)
    )
    if invalid_coordinates.any():
        raise ValueError("GeoLibre export received invalid authoritative latitude/longitude values")

    if export_frame["polling_unit"].isna().any() or export_frame["voter_turnout"].isna().any():
        raise ValueError("GeoLibre export requires non-null polling-unit and turnout values")

    return export_frame


def export_election_results_to_geolibre(
    df: pd.DataFrame,
    output_path: str = "election_map.html",
) -> str:
    """Generate a standalone map from validated, authoritative election results."""
    export_frame = _validate_export_frame(df)
    election_map = geolibre.Map(center=[9.0820, 8.6753], zoom=6)
    election_map.add_basemap("CartoDB.Positron")
    election_map.add_points_from_xy(
        export_frame,
        x="longitude",
        y="latitude",
        popup=["polling_unit", "voter_turnout"],
        layer_name="Election Results",
    )
    election_map.to_html(output_path)
    return output_path
