# Windows autostart

`myshare service install` creates a **Scheduled Task** named `MyShare` that runs
at logon, with least privilege (`/RL LIMITED`) and no administrator rights.

Manual equivalent (PowerShell):

```powershell
$exe = "$env:LOCALAPPDATA\Programs\MyShare\myshare.exe"
$tr  = "`"$exe`" --host 127.0.0.1 --port 8787 --data-dir `"$env:USERPROFILE\MyShare`""
schtasks /Create /F /SC ONLOGON /TN MyShare /TR $tr /RL LIMITED
schtasks /Run /TN MyShare
```

Manage it:

```powershell
myshare service status
myshare service stop
myshare service start
myshare service uninstall
```

Or use Task Scheduler (`taskschd.msc`) and look for the `MyShare` task.
