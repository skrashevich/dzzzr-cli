# dzzzr organizer: browse games, levels and teams through the mock admin area
# Runs a private dzzzr-mock on 127.0.0.1:18094. Admin account is admin/admin.
# Pause guide: short ~3s, medium ~4.5s, large ~6s

Output ../gifs/dzzzr-admin.gif

Source config.tape

Require dzzzr
Require dzzzr-mock

Hide
Type `export PATH="$PWD/bin:$PATH" DZZZR_BASE_URL="http://127.0.0.1:18094/moscow/"`
Enter
Type `export DZZZR_ADMIN_LOGIN=admin DZZZR_ADMIN_PASSWORD=admin`
Enter
Type "dzzzr-mock -addr 127.0.0.1:18094 >/tmp/dzzzr-mock-admin.log 2>&1 &"
Enter
Sleep 2s
Type "dzzzr logout 2>/dev/null; dzzzr login -login demo -password demo -captain demo -pin 1234 >/dev/null"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "# dzzzr — организаторская часть: игры, уровни и команды"
Enter
Sleep 1.5s

Type "dzzzr admin-games"
Enter
Sleep 4s

Type "dzzzr admin-game-info 4242"
Enter
Sleep 5s

Type "dzzzr admin-levels 4242"
Enter
Sleep 5s

Type "dzzzr admin-teams 4242"
Enter
Sleep 5s

Hide
Type "kill %1 2>/dev/null || true"
Enter
