"""Şablon ve görünümlerdeki Türkçe metinlerin İngilizce karşılığı var mı kontrol eder.

Kullanım: python check_i18n.py
"""
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from portal.i18n import EN  # noqa: E402

root = pathlib.Path(__file__).parent
found = set()
for f in (root / "templates").rglob("*.html"):
    found |= set(re.findall(r"""\{%\s*t\s+"([^"]+)"|\{%\s*t\s+'([^']+)'""", f.read_text()) and
                 [a or b for a, b in re.findall(r"""\{%\s*t\s+"([^"]+)"|\{%\s*t\s+'([^']+)'""", f.read_text())])
for f in (root / "portal").glob("*.py"):
    src = f.read_text()
    found |= set(re.findall(r'_\(request,\s*"([^"]+)"', src))
    found |= set(re.findall(r'tr\(lang,\s*"([^"]+)"', src))
    found |= set(re.findall(r'msg = "([^"]+)" if roles', src))
for v in ["Öğrenci", "Teknik ekip", "Yönetici", "Yeni", "İşlemde", "Çözüldü", "Düşük", "Orta", "Acil"]:
    found.add(v)
missing = sorted(s for s in found if s not in EN)
print(f"{len(found)} metin, {len(missing)} eksik")
for m in missing:
    print("  -", m)
sys.exit(1 if missing else 0)
