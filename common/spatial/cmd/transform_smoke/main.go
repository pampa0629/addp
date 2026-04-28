package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/addp/common/spatial"
)

func main() {
	ctx := context.Background()

	input := map[string]interface{}{
		"type":        "Point",
		"coordinates": []float64{12958412.49, 4852030.63},
	}

	result, err := spatial.TransformGeoJSONToWGS84(ctx, input, spatial.SRIDWebMercator, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "transform failed: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}
