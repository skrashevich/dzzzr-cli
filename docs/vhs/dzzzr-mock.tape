# dzzzr-mock: local engine mock for development, dzzzr talking to it
# Runs a private dzzzr-mock on 127.0.0.1:18093.
# Pause guide: short ~3.5s, medium ~5s, large ~7s

Output ../gifs/dzzzr-mock.gif

Source config.tape

Require dzzzr
Require dzzzr-mock

Hide
Type `export PATH="$PWD/bin:$PATH" DZZR_BASE_URL="http://127.0.0.1:18093/moscow/"`
Enter
Type "dzzzr-mock -addr 127.0.0.1:18093 >/tmp/dzzzr-mock-server.log 2>&1 &"
Enter
Sleep 2s
Type "dzzzr logout 2>/dev/null; dzzzr login -login demo -password demo -captain demo -pin 1234 >/dev/null"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# dzzzr-mock — mock-сервер движка «Дозор Классик» для разработки"
Enter
Sleep 1.5s

Type "dzzzr-mock -version"
Enter
Sleep 2.5s

Type "# каждый запрос сервер пишет строкой в stderr:"
Enter
Sleep 1.5s

Type "dzzzr status >/dev/null; dzzzr send-code 1 >/dev/null; tail -n 6 /tmp/dzzzr-mock-server.log"
Enter
Sleep 6s

Type "dzzzr games -archive"
Enter
Sleep 4s

Hide
Type "kill %1 2>/dev/null || true"
Enter
