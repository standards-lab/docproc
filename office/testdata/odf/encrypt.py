# encrypt saves the document at its first argument as an ODF text document, encrypted with the
# password "docproc", at its second, through a soffice of its own, its user profile at the third.
# See office/testdata/README.md
import os
import subprocess
import sys
import time

import uno
from com.sun.star.beans import PropertyValue


def prop(name, value):
    p = PropertyValue()
    p.Name, p.Value = name, value
    return p


source, target, profile = sys.argv[1:4]
pipe = "docproc_encrypt_%d" % os.getpid()
soffice = subprocess.Popen(
    ["soffice", "-env:UserInstallation=file://" + profile, "--headless", "--norestore",
     "--accept=pipe,name=%s;urp;" % pipe],
)
local = uno.getComponentContext()
resolver = local.ServiceManager.createInstanceWithContext("com.sun.star.bridge.UnoUrlResolver", local)
for _ in range(150):
    try:
        remote = resolver.resolve("uno:pipe,name=%s;urp;StarOffice.ComponentContext" % pipe)
        break
    except Exception:
        time.sleep(0.2)
else:
    soffice.kill()
    sys.exit("encrypt: soffice never accepted the connection")
desktop = remote.ServiceManager.createInstanceWithContext("com.sun.star.frame.Desktop", remote)
doc = desktop.loadComponentFromURL(uno.systemPathToFileUrl(source), "_blank", 0, (prop("Hidden", True),))
doc.storeToURL(uno.systemPathToFileUrl(target), (prop("FilterName", "writer8"), prop("Password", "docproc")))
doc.close(True)
try:
    desktop.terminate()
except Exception:
    pass
soffice.wait(timeout=60)
