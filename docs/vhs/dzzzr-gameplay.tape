# dzzzr gameplay: take level 1 (codes 1, 2, 3), watch the level advance, read stat and log
# Runs a private dzzzr-mock on 127.0.0.1:18092.
# Pause guide: short ~3s, medium ~4.5s, large ~6s

Output ../gifs/dzzzr-gameplay.gif

Source config.tape

Require dzzzr
Require dzzzr-mock

Hide
Type `export PATH="$PWD/bin:$PATH" DZZZR_BASE_URL="http://127.0.0.1:18092/moscow/"`
Enter
Type "dzzzr-mock -addr 127.0.0.1:18092 >/tmp/dzzzr-mock-gameplay.log 2>&1 &"
Enter
Sleep 2s
Type "dzzzr logout 2>/dev/null; dzzzr login -login demo -password demo -captain demo -pin 1234"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# dzzzr — проходим первый уровень и смотрим статистику"
Enter
Sleep 1.5s

Type "dzzzr send-code 1"
Enter
Sleep 2.5s

Type "dzzzr send-code 2"
Enter
Sleep 2.5s

Type "dzzzr send-code 3"
Enter
Sleep 3.5s

Type "dzzzr status"
Enter
Sleep 5s

Type "dzzzr stat"
Enter
Sleep 4.5s

Type "dzzzr log"
Enter
Sleep 6s

Hide
Type "kill %1 2>/dev/null || true"
Enter
