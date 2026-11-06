#!/usr/bin/env python3
"""Upload small generated fixtures and verify conversion plus document ACL."""
import io
import json
import urllib.request
import zipfile
from local_config import ROOT, configuration
from check_api import request, tokens


def docx():
    output = io.BytesIO()
    xml = '''<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Novex DOCX fixture</w:t></w:r></w:p>
    <w:tbl><w:tr><w:tc><w:p><w:r><w:t>SKU</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Warranty</w:t></w:r></w:p></w:tc></w:tr>
    <w:tr><w:tc><w:p><w:r><w:t>TEST-001</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>12 months</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
    </w:body></w:document>'''
    with zipfile.ZipFile(output, 'w') as archive:
        archive.writestr('word/document.xml', xml)
        archive.writestr('[Content_Types].xml', '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>')
    return output.getvalue()


def pdf():
    stream = b'BT /F1 12 Tf 50 750 Td (NOVEX_PDF_FIXTURE warranty 12 months) Tj ET'
    objects = [b'<< /Type /Catalog /Pages 2 0 R >>',
               b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
               b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
               b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
               b'<< /Length ' + str(len(stream)).encode() + b' >>\nstream\n' + stream + b'\nendstream']
    output = bytearray(b'%PDF-1.4\n')
    offsets = [0]
    for i, obj in enumerate(objects, 1):
        offsets.append(len(output))
        output += str(i).encode() + b' 0 obj\n' + obj + b'\nendobj\n'
    xref = len(output)
    output += b'xref\n0 6\n0000000000 65535 f \n'
    for offset in offsets[1:]:
        output += f'{offset:010d} 00000 n \n'.encode()
    output += f'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n'.encode()
    return bytes(output)


def upload(filename, data):
    boundary = 'NovexIntegrationDocumentBoundary'
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="scope"\r\n\r\nself\r\n'
            f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\nContent-Type: application/octet-stream\r\n\r\n').encode() + data + f'\r\n--{boundary}--\r\n'.encode()
    req = urllib.request.Request('http://127.0.0.1:8310/api/novex/knowledge', data=body,
                                 headers={'Authorization': 'Bearer ' + tokens['a'], 'Content-Type': 'multipart/form-data; boundary=' + boundary})
    with urllib.request.urlopen(req, timeout=45) as result:
        return json.load(result)['data']


def main():
    password = configuration()['BOOTSTRAP_ADMIN_PASSWORD']
    for user in ('a', 'b'):
        tokens[user] = request('/login', data={'username': user, 'password': password})['token']
    fixtures = [('verification.md', '# NOVEX_MD_FIXTURE\n\n私有文档只对上传者开放。'.encode(), 'NOVEX_MD_FIXTURE'),
                ('verification.txt', b'NOVEX_TXT_FIXTURE', 'NOVEX_TXT_FIXTURE'),
                ('verification.docx', docx(), '| SKU | Warranty |'),
                ('verification.pdf', pdf(), 'NOVEX_PDF_FIXTURE')]
    completed = []
    for name, contents, marker in fixtures:
        doc = upload(name, contents)
        try:
            converted = request('/knowledge/' + doc['id'], 'a')['markdown']
            assert marker in converted, name + ': lost content during conversion'
            if name.endswith('.pdf'):
                assert '第 1 页' in converted, 'PDF page source marker missing'
            request('/knowledge/' + doc['id'], 'b', expected=404)
            completed.append(name)
            print('PASS', name, 'converted and other user denied')
        finally:
            request('/knowledge/' + doc['id'], 'a', method='DELETE')
    (ROOT / '.local/document-verification.json').write_text(json.dumps({'passed': True, 'formats': completed}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
