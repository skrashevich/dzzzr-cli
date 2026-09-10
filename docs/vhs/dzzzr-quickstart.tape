# dzzzr quickstart: login -> games -> status -> level -> hints -> send-code
# Runs a private dzzzr-mock on 127.0.0.1:18091.
# Pause guide: short ~3s, medium ~4.5s, large ~6s

Output ../gifs/dzzzr-quickstart.gif

Source config.tape

Require dzzzr
Require dzzzr-mock

Hide
Type `export PATH="$PWD/bin:$PATH" DZZZR_BASE_URL="http://127.0.0.1:18091/moscow/"`
Enter
Type "dzzzr-mock -addr 127.0.0.1:18091 >/tmp/dzzzr-mock-quickstart.log 2>&1 &"
Enter
Sleep 2s
Type "dzzzr logout 2>/dev/null; dzzzr login -login demo -password demo -captain demo -pin 1234"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# dzzzr — вход, список игр, статус, задание и отправка кода"
Enter
Sleep 1.5s

Type "dzzzr games"
Enter
Sleep 3.5s

Type "dzzzr status"
Enter
Sleep 6s

Type "dzzzr level"
Enter
Sleep 5s

Type "dzzzr hints"
Enter
Sleep 4s

Type "dzzzr send-code 1"
Enter
Sleep 3s

Type "dzzzr send-code НЕВЕРНЫЙ"
Enter
Sleep 4s

Hide
Type "kill %1 2>/dev/null || true"
Enter
