# Chistore-go

Менеджер пакетов для небольших консольных утилит. Написан на Go, ставится одним
бинарником в домашний каталог, root не нужен.

Команда называется `chistore`. В этом репозитории лежит только менеджер. Пакеты
хранятся в отдельных репозиториях.

[English](README.md)

## Названия

Три названия легко перепутать, поэтому коротко:

- Chistore-go: этот проект, менеджер.
- chistore-repo: основной репозиторий пакетов. Подключается после установки.
- Chistore: старый репозиторий пакетов и Bash-менеджер. Заархивирован.

Внутри менеджера пакетов нет. Он скачивает из репозитория `index.json`, находит
там нужный пакет и качает его файлы оттуда же.

Зеркала на GitHub: [Chistore-go](https://github.com/name-cho/Chistore-go),
[chistore-repo](https://github.com/name-cho/chistore-repo),
[Chistore](https://github.com/name-cho/Chistore).

## Возможности

- один статический бинарник, зависимостей у менеджера нет
- несколько репозиториев с приоритетами в `repos.json`
- читает `index.json`; старые репозитории с `INDEX` тоже работают
- проверяет SHA-256 файлов, если в индексе есть хеши
- ставит в `$XDG_BIN_HOME` (по умолчанию `~/.local/bin`), root не нужен
- импортирует старый `~/.local/share/chistore/manifest`
- обновляется из GitHub Releases и проверяет SHA-256 перед заменой бинарника

## Установка

Linux, macOS и WSL:

```bash
curl -fsSL https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.sh -o /tmp/chistore-install.sh
sed -n '1,120p' /tmp/chistore-install.sh
bash /tmp/chistore-install.sh
```

В минимальном Arch WSL обычно нет `less`. Там работает `sed` или `cat`.

Если curl падает с `TLS connect error: error:00000000:lib(0)::reason(0)`:

```bash
sudo pacman -S --needed ca-certificates curl
curl -4 -fsSL https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.sh -o /tmp/chistore-install.sh
```

Опция `-4` включает IPv4 и помогает, когда IPv6 не работает. Если не помогло,
проверьте `date -u` и попробуйте зеркало GitGud:

```bash
curl -4 -fsSL https://gitgud.io/name-cho/Chistore-go/-/raw/main/scripts/install.sh -o /tmp/chistore-install.sh
bash /tmp/chistore-install.sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.ps1 -OutFile "$env:TEMP\chistore-install.ps1"
Get-Content "$env:TEMP\chistore-install.ps1"
powershell -ExecutionPolicy Bypass -File "$env:TEMP\chistore-install.ps1"
```

Можно скачать готовый бинарник:

```bash
curl -fsSL https://github.com/name-cho/Chistore-go/releases/latest/download/chistore_linux_amd64 \
  -o ~/.local/bin/chistore
chmod +x ~/.local/bin/chistore
```

Остальные файлы: `linux_arm64`, `darwin_amd64`, `darwin_arm64`,
`windows_amd64.exe`, `windows_arm64.exe`.

Установщики кладут `chistore` в `~/.local/bin`
(`%LOCALAPPDATA%\Programs\chistore\bin` на Windows). Если каталога нет в `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
. ~/.bashrc
```

## Переход со старой Bash-версии

Поставьте Chistore-go и запустите `chistore list`. При первом запуске он
импортирует старый `~/.local/share/chistore/manifest`, поэтому установленные
пакеты останутся в списке. Названия пакетов не менялись. Старый репозиторий
Chistore и Bash-менеджер заархивированы, а в `chistore-repo` лежат те же пакеты.

## Использование

```bash
chistore search
chistore install envsnap
chistore list
chistore info envsnap
chistore upgrade
chistore remove envsnap
chistore purge envsnap
chistore self-update
```

Старые флаги работают:

| Флаг | Действие |
| --- | --- |
| `-h`, `--help` | показать справку |
| `-Ss [запрос]` | поиск по репозиториям |
| `-S <пакет...>` | установить пакеты |
| `-Syu` | обновить установленные пакеты |
| `-Rn <пакет...>` | удалить, конфиги и данные оставить |
| `-Rns <пакет...>` | удалить полностью |
| `-Qet` | список установленных пакетов |
| `-Qs [запрос]` | поиск среди установленных |
| `-Qi <пакет>` | информация о пакете |

Если пакет есть в нескольких репозиториях, добавьте `@repo`:

```bash
chistore install envsnap@gitgud
chistore info envsnap@github
```

Глобальные опции:

```bash
chistore --yes install envsnap
chistore --no-color list
chistore --repo URL search
chistore --no-self-update install envsnap
```

## Репозитории

После установки настроен один репозиторий `gitgud`, он указывает на
`chistore-repo`:

```json
{
  "schema": 1,
  "repos": [
    {
      "name": "gitgud",
      "url": "https://gitgud.io/name-cho/chistore-repo/-/raw/main",
      "priority": 10,
      "enabled": true
    }
  ]
}
```

Где лежит конфиг:

- Linux, macOS, WSL: `~/.config/chistore/repos.json`
- Windows: `%APPDATA%\chistore\repos.json`

Меньший приоритет выигрывает, если пакет есть в двух репозиториях. Отключённый
репозиторий остаётся в файле, но пропускается.

Список можно менять командами:

```bash
chistore repo add github https://raw.githubusercontent.com/name-cho/chistore-repo/main --priority 20
chistore repo list
chistore repo remove github
chistore repo refresh
```

Пример добавляет зеркало на GitHub. У него приоритет 20, поэтому при дублях
по-прежнему выигрывает копия с GitGud.

Установленный пакет помнит, откуда он пришёл. `upgrade` берёт новую версию из
того же репозитория, даже если у другого приоритет меньше. У пакетов, поставленных
старыми версиями, источника нет, для них работает приоритет.

`--repo` и `CHISTORE_REPO` заменяют список на один репозиторий для одной команды:

```bash
chistore --repo https://gitgud.io/name-cho/chistore-repo/-/raw/main search
```

### Почему raw-ссылка отдаёт 404

Базовый raw-адрес это не файл:

```text
https://gitgud.io/name-cho/chistore-repo/-/raw/main             -> 404
https://gitgud.io/name-cho/chistore-repo/-/raw/main/index.json -> 200
```

GitLab и GitHub отдают по raw-адресу файлы и не показывают список каталога.
Поэтому браузер на базовом адресе получает 404, хотя сам репозиторий работает.
`chistore` базовый адрес не запрашивает. С настройками по умолчанию он просит:

1. `<base>/index.json`
2. `<base>/<пакет>/<файл>` для нужных пакетов

Если `index.json` нет, он пробует `<base>/INDEX`, потом
`<base>/<пакет>/info.md`. Ошибка 404 на базовом адресе установке не мешает.

Посмотреть репозиторий можно на его странице:

- <https://gitgud.io/name-cho/chistore-repo>
- <https://github.com/name-cho/chistore-repo>

## Репозиторий пакетов

Репозиторий пакетов это обычный Git-репозиторий с `index.json` и каталогом на
каждый пакет:

```text
chistore-repo/
├── index.json
├── envsnap/
│   ├── info.md
│   └── envsnap
└── pystat/
    ├── info.md
    └── pystat.py
```

Пример `index.json`:

```json
{
  "schema": 1,
  "name": "chistore-repo",
  "generated": "2026-10-27T08:58:42Z",
  "packages": [
    {
      "id": "envsnap",
      "name": "EnvSnap",
      "description": "Snapshot system & environment state with diff support",
      "version": "1.0.1",
      "size": "3.8K",
      "type": "single",
      "main": "envsnap",
      "sha256": "e6107bc55160ee4b328cbb5d28dce0d8fcb9d336fd2bb245fee24651db93cd00",
      "author": "name-cho",
      "homepage": "https://gitgud.io/name-cho",
      "source": "https://gitgud.io/name-cho/chistore-repo/-/raw/main/envsnap",
      "dependencies": [{ "manager": "sys", "packages": ["bash"] }]
    }
  ]
}
```

Новым репозиториям нужен только `index.json`. В старых может быть ещё `INDEX`,
простой список id пакетов. Chistore читает его, если `index.json` нет, а `pkg
index` обновляет его только когда файл уже существует.

`info.md` нужен для `pkg init`, `pkg index` и `pkg validate`. Клиент читает
`index.json` и не скачивает `info.md` при установке.

## Как добавить пакет

Работайте в клоне репозитория пакетов:

```bash
chistore pkg init hello
```

Команда создаёт `hello/info.md`, шаблон исполняемого файла и пересобирает оба
индекса. Заполните `info.md`, замените файл, потом выполните:

```bash
chistore pkg validate
chistore pkg index
```

`pkg index` также считает SHA-256 каждого файла пакета.

Формат `info.md`:

```markdown
## Name: Hello
## Description: Example utility
## Version: 1.0.0
## Size: 1K
## Type: single
## MainFile: hello
## Author: your-name
## Homepage: https://example.com
## Dependencies: sys:bash,curl pip:rich
```

Строки без `##` тоже подходят. Неизвестные поля игнорируются, поэтому новые
метаданные не ломают старые клиенты.

Типы пакетов:

- `single`: один исполняемый файл, `<пакет>/<MainFile>`
- `python`: Python-файл в общий virtualenv chistore
- `archive`: `<пакет>.tar.gz`, распаковывается в каталог данных пакета

Менеджеры зависимостей:

- `sys:` (также `pacman:`, `apt:`, `dnf:`) для системных пакетов
- `pip:` (также `python:`) для `~/.local/share/chistore/venv`
- `npm:` (также `node:`) для `npm install -g`

На Windows держите `.gitattributes` с `* text=auto eol=lf`. Git должен отдавать
LF-байты. `pkg index` хеширует файл в том виде, в каком его отдаёт Git, и CRLF в
рабочей копии даёт хеши, которые не сходятся при установке.

## Windows

Менеджер работает на Windows: установка, список, информация, обновление,
удаление и команды `pkg`. Сами пакеты это shell- или Python-программы, поэтому
для запуска нужен Git Bash, WSL или другой `/bin/sh`.

## Переменные окружения

| Переменная | Значение |
| --- | --- |
| `CHISTORE_REPO` | использовать только этот репозиторий, игнорировать `repos.json` |
| `CHISTORE_UPDATE_URL` | URL GitHub Releases API для самообновления |
| `CHISTORE_NO_AUTO_UPDATE=1` | отключить ежедневную проверку обновлений |
| `XDG_DATA_HOME` | каталог данных, по умолчанию `~/.local/share` |
| `XDG_CONFIG_HOME` | каталог конфигов, по умолчанию `~/.config` |
| `XDG_BIN_HOME` | каталог для бинарников, по умолчанию `~/.local/bin` |

Установочные скрипты также читают `CHISTORE_BIN_DIR` и `CHISTORE_RELEASE_REPO`.

## Самообновление

`chistore` проверяет GitHub Releases раз в день и заменяет себя. Перед заменой он
сверяет SHA-256 из `checksums.txt`. Текущий процесс работает со старым кодом до
следующего запуска.

Обновить вручную:

```bash
chistore self-update
```

## Совместимость

- `info.md`, пакеты single, Python и archive работают как раньше.
- Репозитории без `index.json` читаются через `INDEX` и `info.md`. В новых
  репозиториях `INDEX` не создаётся, `pkg index` обновляет его только если файл
  уже есть.
- `repos.json`, который указывает на старый репозиторий по умолчанию
  (`gitgud.io/name-cho/Chistore/-/raw/main`), переключается на `chistore-repo` при
  загрузке. Конфиги с другими репозиториями или своим URL не меняются.
- Старый `manifest` с разделителями-трубками импортируется в `state.json` при
  первом запуске и переименовывается в `manifest.migrated`.
- Установленные бинарники отслеживаются и удаляются только из каталога chistore.
  Purge не трогает пути вне каталогов конфигов и данных.
- Распаковка архивов отклоняет абсолютные пути, `..`, Windows-пути с диском и
  ссылки.

## Сборка и тесты

```bash
go test ./...
go vet ./...
make build
```

Release workflow собирает `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`, `windows/amd64` и `windows/arm64`.

## Удаление

```bash
rm -f ~/.local/bin/chistore
rm -rf ~/.local/share/chistore ~/.config/chistore
```

Если нужно удалить и поставленные утилиты:

```bash
chistore list
chistore purge <пакет>
```
