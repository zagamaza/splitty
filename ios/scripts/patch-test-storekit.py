#!/usr/bin/env python3
"""Прописывает StoreKit-конфиг в TestAction схемы.

XcodeGen умеет класть `storeKitConfiguration` только в LaunchAction (запуск), а
UI-тесты запускают приложение через TestAction — и без конфига там экран оплаты
показывает «не удалось загрузить тарифы»: цены брать неоткуда, App Store
Connect в симуляторе недоступен.

Скрипт идемпотентен и вызывается из `options.postGenCommand`, поэтому правка не
теряется при каждой регенерации проекта.
"""
import pathlib
import re
import sys

SCHEME = pathlib.Path(__file__).resolve().parent.parent / (
    "Splitty.xcodeproj/xcshareddata/xcschemes/Splitty.xcscheme"
)
# Путь относителен КАТАЛОГУ .xcscheme (Splitty.xcodeproj/xcshareddata/xcschemes),
# поэтому до ios/ подниматься надо на три уровня. XcodeGen пишет два — и его
# ссылку storekitd молча игнорирует, уходя за ценами в настоящий App Store
# (в логе видно «Requesting via MediaAPI»). Поэтому чиним обе ссылки, а не
# только тестовую.
STOREKIT_PATH = "../../../Splitty/Splitty.storekit"
REFERENCE = (
    '      <StoreKitConfigurationFileReference\n'
    f'         identifier = "{STOREKIT_PATH}">\n'
    '      </StoreKitConfigurationFileReference>\n'
)


def main() -> int:
    if not SCHEME.exists():
        print(f"схемы нет: {SCHEME}", file=sys.stderr)
        return 0  # не роняем генерацию: схему могли не собирать

    text = SCHEME.read_text(encoding="utf-8")

    # XcodeGen пишет путь на уровень выше нужного — правим и его ссылку тоже.
    text = text.replace(
        'identifier = "../../Splitty/Splitty.storekit"',
        f'identifier = "{STOREKIT_PATH}"',
    )

    test_action = re.search(r"<TestAction\b.*?</TestAction>", text, re.S)
    if not test_action:
        print("в схеме нет TestAction — пропускаю", file=sys.stderr)
        return 0

    block = test_action.group(0)
    if "StoreKitConfigurationFileReference" in block:
        SCHEME.write_text(text, encoding="utf-8")  # путь мог поправиться выше
        return 0

    patched = block.replace("   </TestAction>", REFERENCE + "   </TestAction>", 1)
    if patched == block:
        print("не нашёл, куда вставить ссылку на StoreKit-конфиг", file=sys.stderr)
        return 0

    SCHEME.write_text(text.replace(block, patched, 1), encoding="utf-8")
    print("StoreKit-конфиг прописан в TestAction")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
