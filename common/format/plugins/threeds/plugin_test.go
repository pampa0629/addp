package threeds

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func chunk(id uint16, body []byte) []byte {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint16(buf, id)
	binary.LittleEndian.PutUint32(buf[2:], uint32(6+len(body)))
	return append(buf, body...)
}
func fixture() []byte {
	vertices := make([]byte, 2+36)
	binary.LittleEndian.PutUint16(vertices, 3)
	faces := make([]byte, 2+8)
	binary.LittleEndian.PutUint16(faces, 1)
	mesh := chunk(0x4100, append(chunk(0x4110, vertices), chunk(0x4120, faces)...))
	object := chunk(0x4000, append([]byte("triangle\x00"), mesh...))
	material := chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte("texture.png\x00"))))
	return chunk(0x4d4d, chunk(0x3d3d, append(object, material...)))
}
func Test3DSSummaryReadsChunks(t *testing.T) {
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(fixture()), nil)
	if err != nil {
		t.Fatal(err)
	}
	info := result.Model3D
	if *info.MeshCount != 1 || *info.VertexCount != 3 || *info.TriangleCount != 1 || *info.MaterialCount != 1 {
		t.Fatalf("summary=%+v", info)
	}
	if result.FormatInfo["texture_refs"].([]string)[0] != "texture.png" || result.FormatInfo["has_animation"] != false {
		t.Fatalf("facts=%v", result.FormatInfo)
	}
}
func Test3DSRejectsInvalidChunkLengths(t *testing.T) {
	malformed := fixture()
	binary.LittleEndian.PutUint32(malformed[8:], 0xffffffff)
	for _, data := range [][]byte{nil, fixture()[:5], fixture()[:len(fixture())-1], chunk(0x4d4d, []byte("oops")), malformed} {
		if _, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(data), nil); err == nil {
			t.Fatal("accepted malformed 3DS")
		}
	}
}
