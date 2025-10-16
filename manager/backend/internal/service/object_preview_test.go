package service

import "testing"

func TestInferContentType(t *testing.T) {
	testcases := []struct {
		name        string
		objectPath  string
		contentType string
		expect      string
	}{
		{
			name:        "keep_explicit_type",
			objectPath:  "bucket/report.pdf",
			contentType: "application/pdf",
			expect:      "application/pdf",
		},
		{
			name:        "docx_with_generic_type",
			objectPath:  "bucket/docs/关于底座.docx",
			contentType: "application/octet-stream",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "docx_uppercase_extension_generic_type",
			objectPath:  "bucket/docs/Manual.DOCX",
			contentType: "APPLICATION/OCTET-STREAM",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "docx_non_mime_token_fallback",
			objectPath:  "bucket/docs/公共技术底座部署手册.docx",
			contentType: "docx",
			expect:      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:        "wps_extension_generic_type",
			objectPath:  "bucket/docs/示例文档.wps",
			contentType: "application/octet-stream",
			expect:      "application/vnd.ms-works",
		},
		{
			name:        "unknown_extension_keeps_generic_type",
			objectPath:  "bucket/blob/data.bin",
			contentType: "application/octet-stream",
			expect:      "application/octet-stream",
		},
		{
			name:        "binary_octet_stream_treated_as_generic",
			objectPath:  "bucket/slides/demo.pptx",
			contentType: "binary/octet-stream",
			expect:      "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := inferContentType(tc.objectPath, tc.contentType)
			if got != tc.expect {
				t.Fatalf("inferContentType(%q, %q) = %q, want %q", tc.objectPath, tc.contentType, got, tc.expect)
			}
		})
	}
}

func TestObjectContentMatcherGenericContentType(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{".docx"},
		[]string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "wordprocessingml"},
	)
	req := &ObjectContentRequest{
		Extension:   ".docx",
		ContentType: "docx",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to accept generic DOCX content type")
	}
}

func TestObjectContentMatcherWPS(t *testing.T) {
	t.Parallel()
	matcher := newObjectContentMatcher(
		[]string{".wps"},
		[]string{"application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps"},
	)
	req := &ObjectContentRequest{
		Extension:   ".wps",
		ContentType: "application/wps-office.doc",
	}
	if !matcher.matches(req) {
		t.Fatalf("expected matcher to accept WPS content type")
	}
}
