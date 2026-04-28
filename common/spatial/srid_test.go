package spatial

import "testing"

func TestParseSRID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		crs  string
		want int
	}{
		{
			name: "explicit epsg",
			crs:  "EPSG:4326",
			want: SRIDWGS84,
		},
		{
			name: "web mercator wkt",
			crs:  `PROJCS["WGS 84 / Pseudo-Mercator",GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]],PROJECTION["Mercator_1SP"],PARAMETER["central_meridian",0],PARAMETER["scale_factor",1],PARAMETER["false_easting",0],PARAMETER["false_northing",0],UNIT["metre",1],AXIS["X",EAST],AXIS["Y",NORTH],AUTHORITY["EPSG","3857"]]`,
			want: SRIDWebMercator,
		},
		{
			name: "utm zone 50n from real sample",
			crs:  `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`,
			want: 32650,
		},
		{
			name: "projected wgs84 without explicit mapping should not guess 4326",
			crs:  `PROJCS["Custom WGS 84 Projected",GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["central_meridian",117],UNIT["metre",1]]`,
			want: 0,
		},
		{
			name: "geographic wgs84 fallback",
			crs:  `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`,
			want: SRIDWGS84,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseSRID(tt.crs); got != tt.want {
				t.Fatalf("ParseSRID() = %d, want %d", got, tt.want)
			}
		})
	}
}
