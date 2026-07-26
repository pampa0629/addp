package datatype

import "testing"

func TestContainerInfoCloneDeepCopiesRowCounts(t *testing.T) {
	rowCount := int64(0)
	estimatedRowCount := int64(12)
	info := &ContainerInfo{
		Children: []ContainerChildInfo{{
			Name:              "Sheet1",
			RowCount:          &rowCount,
			EstimatedRowCount: &estimatedRowCount,
		}},
	}

	cloned := info.Clone()
	if cloned == nil || len(cloned.Children) != 1 {
		t.Fatalf("Clone() = %#v", cloned)
	}
	*cloned.Children[0].RowCount = 5
	*cloned.Children[0].EstimatedRowCount = 20

	if *info.Children[0].RowCount != 0 || *info.Children[0].EstimatedRowCount != 12 {
		t.Fatalf("original counts changed: %#v", info.Children[0])
	}
}
