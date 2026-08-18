import { describe, expect, it } from 'vitest'
import {
  formatXMLForDisplay,
  isXMLTextPreview,
  withFormattedXMLPreview
} from '../../src/utils/xmlPreview'

const xmlPreview = (text, format = 'xml') => ({
  object: {
    attributes: { item: { format } },
    content: { kind: 'text', text }
  }
})

describe('formatXMLForDisplay', () => {
  it('indents nested XML and preserves the declaration and attributes', () => {
    const source = '<?xml version="1.0" encoding="UTF-8"?><catalog><item id="1"><name>Road</name></item></catalog>'

    expect(formatXMLForDisplay(source)).toBe(`<?xml version="1.0" encoding="UTF-8"?>
<catalog>
  <item id="1">
    <name>Road</name>
  </item>
</catalog>`)
  })

  it('preserves comments, CDATA, and mixed text without injecting whitespace', () => {
    const source = '<root><!-- note --><script><![CDATA[a < b]]></script><label>Hello <b>XML</b>!</label></root>'

    expect(formatXMLForDisplay(source)).toBe(`<root>
  <!-- note -->
  <script><![CDATA[a < b]]></script>
  <label>Hello <b>XML</b>!</label>
</root>`)
  })

  it('returns malformed XML unchanged', () => {
    const source = '<root><item></root>'
    expect(formatXMLForDisplay(source)).toBe(source)
  })
})

describe('XML preview selection', () => {
  it('uses confirmed content metadata before the parent item format', () => {
    const data = xmlPreview('<metadata/>', 'shapefile')
    data.object.content.metadata = { format: 'xml' }

    expect(isXMLTextPreview(data)).toBe(true)
  })

  it('does not treat ordinary text as XML', () => {
    expect(isXMLTextPreview(xmlPreview('<root/>', 'txt'))).toBe(false)
  })

  it('formats XML by default and keeps the original data immutable', () => {
    const data = xmlPreview('<root><item>1</item></root>')
    const displayed = withFormattedXMLPreview(data)

    expect(displayed.object.content.text).toBe('<root>\n  <item>1</item>\n</root>')
    expect(data.object.content.text).toBe('<root><item>1</item></root>')
  })

  it('returns raw XML when raw mode is enabled', () => {
    const data = xmlPreview('<root><item>1</item></root>')
    expect(withFormattedXMLPreview(data, true)).toBe(data)
  })
})
