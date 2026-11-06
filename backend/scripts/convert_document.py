#!/usr/bin/env python3
"""Bounded text extraction. Scanned PDFs intentionally require OCR upstream."""
import sys, pathlib, zipfile, xml.etree.ElementTree as ET, subprocess, shutil
path = pathlib.Path(sys.argv[1]); ext = path.suffix.lower()
if path.stat().st_size > 16 * 1024 * 1024:
    raise SystemExit('File exceeds 16 MiB')
if ext in ('.md', '.txt'):
    text = path.read_text(encoding='utf-8-sig')
elif ext == '.docx':
    with zipfile.ZipFile(path) as z:
        if sum(i.file_size for i in z.infolist()) > 32 * 1024 * 1024:
            raise SystemExit('Expanded DOCX exceeds 32 MiB')
        doc = ET.fromstring(z.read('word/document.xml'))
    ns = {'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
    lines=[]
    def paragraph(p):
        line=''.join(t.text or '' for t in p.findall('.//w:t',ns))
        style=p.find('./w:pPr/w:pStyle',ns)
        style_name=style.get('{'+ns['w']+'}val','') if style is not None else ''
        if style_name.lower().startswith('heading'):
            suffix=style_name[7:]
            if suffix.isdigit(): line='#'*min(int(suffix),6)+' '+line
        return line.strip()
    body=doc.find('w:body',ns)
    for child in body if body is not None else []:
        if child.tag.endswith('}p'):
            line=paragraph(child)
            if line: lines.append(line)
        elif child.tag.endswith('}tbl'):
            rows=[]
            for row in child.findall('w:tr',ns):
                cells=['; '.join(filter(None,(paragraph(p) for p in cell.findall('w:p',ns)))).replace('|','\\|') for cell in row.findall('w:tc',ns)]
                rows.append(cells)
            if rows:
                width=max(map(len,rows))
                table=[]
                for index,row in enumerate(rows):
                    table.append('| '+' | '.join(row+['']*(width-len(row)))+' |')
                    if index==0: table.append('| '+' | '.join(['---']*width)+' |')
                lines.append('\n'.join(table))
    text='\n\n'.join(lines)
elif ext == '.pdf':
    if shutil.which('pdftotext'):
        raw=subprocess.run(['pdftotext','-layout',str(path),'-'],capture_output=True,check=True,timeout=20).stdout.decode('utf-8')
        pages=raw.split('\f')
        text='\n\n'.join('## 第 '+str(n+1)+' 页\n\n'+page.strip() for n,page in enumerate(pages) if page.strip())
    else:
        try: from pypdf import PdfReader
        except ImportError: raise SystemExit('PDF requires pdftotext or pypdf')
        reader=PdfReader(path)
        if len(reader.pages)>250: raise SystemExit('PDF exceeds 250 pages')
        pages=[(p.extract_text() or '').strip() for p in reader.pages]
        text='\n\n'.join('## 第 '+str(n+1)+' 页\n\n'+page for n,page in enumerate(pages) if page)
else:
    raise SystemExit('Supported types: MD TXT DOCX text PDF')
text=text.replace('\x00','').strip()
if not text: raise SystemExit('No text extracted; scanned PDF requires OCR')
if len(text.encode('utf-8')) > 2 * 1024 * 1024: raise SystemExit('Extracted text exceeds 2 MiB')
print(text)
