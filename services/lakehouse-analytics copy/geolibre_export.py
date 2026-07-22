import geolibre
import pandas as pd
import json
import os

def export_election_results_to_geolibre(df: pd.DataFrame, output_path: str = "election_map.html"):
    """
    Takes a DataFrame with latitude, longitude, and election metrics
    and generates a standalone HTML map using the geolibre Python package.
    """
    m = geolibre.Map(center=[9.0820, 8.6753], zoom=6) # Center on Nigeria
    
    # Add base layer
    m.add_basemap("CartoDB.Positron")
    
    # Ensure dataframe has required columns or mock them
    if 'latitude' not in df.columns or 'longitude' not in df.columns:
        print("Warning: DataFrame missing latitude/longitude columns. Using default locations.")
        # Mock some data for demonstration if missing
        df['latitude'] = [9.0820, 8.5, 9.5]
        df['longitude'] = [8.6753, 8.0, 9.0]
        df['polling_unit'] = ["PU1", "PU2", "PU3"]
        df['voter_turnout'] = [500, 300, 450]
        
    # Add points
    m.add_points_from_xy(
        df, 
        x="longitude", 
        y="latitude", 
        popup=["polling_unit", "voter_turnout"],
        layer_name="Election Results"
    )
    
    # Save the map
    m.to_html(output_path)
    print(f"GeoLibre map exported to {output_path}")
    return output_path

if __name__ == "__main__":
    # Test execution
    df = pd.DataFrame()
    export_election_results_to_geolibre(df, "test_map.html")
