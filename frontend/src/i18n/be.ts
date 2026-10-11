import type { Messages } from './en'

// Belarusian (Cyrillic). Needs review by a native speaker.
export const be: Messages = {
  appName: 'Čysta Dyska',
  tagline: 'Даведайцеся, што займае ваш дыск.',
  language: 'Мова',

  chooseFolder: 'Абраць папку…',
  noFolder: 'Папка не абрана',
  scan: 'Сканаваць',
  cancel: 'Скасаваць',
  rescan: 'Сканаваць зноў',

  scanning: 'Сканаванне…',
  cancelled: 'Сканаванне скасавана.',
  scanFailed: 'Не атрымалася сканаваць: {error}',
  scanDone: 'Прасканавана {size}: {files} файлаў і {dirs} папак.',

  files: 'Файлы',
  folders: 'Папкі',
  size: 'Памер',
  name: 'Назва',
  share: 'Доля',
  elapsed: 'Прайшло',
  unreadable: 'Не прачытана',

  moreItems: '…і яшчэ {count} элементаў',
  accessDenied: 'Не ўдалося прачытаць частку гэтай папкі',
  emptyFolder: 'Гэтая папка пустая.',
  readOnlyNote: 'Толькі чытанне: Čysta Dyska нічога не змяняе і не выдаляе.',
  logicalSizeNote: 'Паказаны лагічны памер файлаў; рэальны памер на дыску — у наступнай версіі.',
  sortBy: 'Сартаваць па: {column}',
  expand: 'Разгарнуць {name}',
  collapse: 'Згарнуць {name}',
}
