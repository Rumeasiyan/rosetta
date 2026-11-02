#!/usr/bin/env python3
"""Writes a small .docx of invented text. Standard library only."""

import sys
import zipfile
from xml.sax.saxutils import escape

PARAGRAPHS = [
    "We recieve the letters.",
    "\U0001f600 They seperate them by date, and we recieve more.",
    "Call 077 123 4567 to recieve a copy.",
]
W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
body = "".join(f"<w:p><w:r><w:t>{escape(p)}</w:t></w:r></w:p>" for p in PARAGRAPHS)
with zipfile.ZipFile(sys.argv[1], "w", zipfile.ZIP_DEFLATED) as z:
    z.writestr(
        "[Content_Types].xml",
        '<?xml version="1.0" encoding="UTF-8"?>'
        '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
        '<Default Extension="rels" '
        'ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
        '<Default Extension="xml" ContentType="application/xml"/>'
        '<Override PartName="/word/document.xml" '
        'ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.docum'
        'ent.main+xml"/></Types>',
    )
    z.writestr(
        "_rels/.rels",
        '<?xml version="1.0" encoding="UTF-8"?>'
        "<Relationships "
        'xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Id="rId1" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeD'
        'ocument" Target="word/document.xml"/></Relationships>',
    )
    z.writestr(
        "word/document.xml",
        f'<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="{W}"><w:body>{body}'
        f"<w:sectPr/></w:body></w:document>",
    )
