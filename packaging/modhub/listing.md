# Mod Hub listing (wgmods.net)

Text for the mod's page. The portal's official language is Russian, and text in another
language needs a Russian translation beside it (portal Terms of Use §4.1), so every field is
given in both. Upload `ondrejkouril.wotctx_<version>.wotmod`, the same file attached to the
Tank Advisor release, so the Mod Hub and the app ship one package. Images: `cover.jpg`
(302×170, the size in the mod list) or `cover@2x.jpg`, and `icon.jpg`.

---

## English

**Title**
Tank Advisor – Garage Reader

**Short description**
Read-only. Saves your garage's tank XP, mark progress, equipment, shells and crew to a file on
your PC, so Claude can advise you from real figures. No network, nothing in battle.

**Description**

Tank Advisor lets Claude (Anthropic's AI assistant) answer questions about your own World of
Tanks account with real figures instead of guesses: *"what tank should I get next?"*, *"how
close am I to my next mark?"*, *"which equipment am I running on this tank?"*.

Wargaming's public API gives out your statistics and garage, but not everything the game
client knows. This mod fills that gap. While you're in the garage, it reads:

- the XP on each tank, and your free XP, credits, gold and bonds;
- Marks of Excellence and your progress towards the next mark;
- each tank's equipment, shells, consumables and directives;
- each tank's crew and their skills;
- which tanks you have researched, and your premium account status.

It writes all of this to one file on your own computer:
`%LOCALAPPDATA%\wotctx\mod\garage.json`. The Tank Advisor app reads that file.

**What it does not do:**
- It makes no network connection of any kind. The file never leaves your PC unless you ask
  Claude about your account.
- It does nothing in battle. It only runs in the garage, after the client has finished
  syncing your account.
- It shows nothing on screen, changes no interface and handles no input.
- It changes nothing in the game.

The mod is useful together with the free Tank Advisor app, which also installs it and puts it
back after each game update: https://github.com/ondrejkouril/tank-advisor

Open source, MIT licence. Tank Advisor is not affiliated with or endorsed by Wargaming.

**Installation**
1. Copy `ondrejkouril.wotctx_0.3.0.wotmod` into `World_of_Tanks\mods\<game version>\` (for
   example `mods\2.4.0.1\`).
2. Start the game and wait a few seconds in the garage. The file appears at
   `%LOCALAPPDATA%\wotctx\mod\garage.json`.

If you use the Tank Advisor app, you don't need this download: the app installs the mod and
keeps it up to date.

**Changelog**
- 0.3.0: also records which tanks you have researched.
- 0.2.1: writes once the garage has settled after each change, rather than at a fixed rate.
- 0.2.0: the full garage: each tank's XP, marks and mark progress, equipment, shells,
  consumables, directives and crew skills, plus premium account and WoT Plus status.
- 0.1.0: first version: credits, gold, bonds and free XP.

---

## Русский

**Название**
Tank Advisor – чтение данных ангара

**Краткое описание**
Только чтение. Сохраняет опыт танков, прогресс отметок, оборудование, снаряды и экипаж из
ангара в файл на вашем ПК, чтобы Claude мог давать советы на основе реальных данных. Без сети,
ничего в бою.

**Описание**

Tank Advisor позволяет Claude (ИИ-ассистенту компании Anthropic) отвечать на вопросы о вашем
собственном аккаунте World of Tanks на основе реальных данных, а не догадок: *«какой танк взять
следующим?»*, *«сколько мне осталось до следующей отметки?»*, *«какое оборудование стоит на
этом танке?»*.

Публичный API Wargaming отдаёт вашу статистику и ангар, но не всё, что знает игровой клиент.
Этот мод восполняет этот пробел. Пока вы находитесь в ангаре, он считывает:

- опыт каждого танка, а также свободный опыт, кредиты, золото и боны;
- отметки на орудии и прогресс до следующей отметки;
- оборудование, снаряды, снаряжение и директивы каждого танка;
- экипаж каждого танка и его навыки;
- какие танки у вас исследованы и статус премиум-аккаунта.

Всё это записывается в один файл на вашем компьютере:
`%LOCALAPPDATA%\wotctx\mod\garage.json`. Этот файл читает приложение Tank Advisor.

**Чего мод не делает:**
- Не устанавливает никаких сетевых соединений. Файл не покидает ваш компьютер, пока вы сами
  не спросите Claude о своём аккаунте.
- Ничего не делает в бою. Работает только в ангаре, после того как клиент синхронизировал
  аккаунт.
- Ничего не показывает на экране, не меняет интерфейс и не обрабатывает ввод.
- Ничего не меняет в игре.

Мод полезен вместе с бесплатным приложением Tank Advisor, которое само устанавливает мод и
возвращает его после каждого обновления игры: https://github.com/ondrejkouril/tank-advisor

Открытый исходный код, лицензия MIT. Tank Advisor не связан с Wargaming и не одобрен ею.

**Установка**
1. Скопируйте `ondrejkouril.wotctx_0.3.0.wotmod` в папку `World_of_Tanks\mods\<версия игры>\`
   (например, `mods\2.4.0.1\`).
2. Запустите игру и подождите несколько секунд в ангаре. Файл появится по пути
   `%LOCALAPPDATA%\wotctx\mod\garage.json`.

Если вы пользуетесь приложением Tank Advisor, этот файл скачивать не нужно: приложение само
устанавливает мод и обновляет его.

**История изменений**
- 0.3.0: также записывает, какие танки исследованы.
- 0.2.1: записывает данные один раз, когда ангар обновился после изменений, а не с
  фиксированной частотой.
- 0.2.0: полные данные ангара: опыт каждого танка, отметки и прогресс до следующей,
  оборудование, снаряды, снаряжение, директивы и навыки экипажа, а также статус
  премиум-аккаунта и WoT Plus.
- 0.1.0: первая версия: кредиты, золото, боны и свободный опыт.
