export const en = {
  appName: 'Čysta Dyska',
  tagline: 'Find out what is eating your disk.',
  language: 'Language',

  chooseFolder: 'Choose folder…',
  noFolder: 'No folder selected',
  scan: 'Scan',
  cancel: 'Cancel',
  rescan: 'Rescan',

  scanning: 'Scanning…',
  cancelled: 'Scan cancelled.',
  scanFailed: 'Scan failed: {error}',
  scanDone: 'Scanned {size} in {files} files and {dirs} folders.',

  files: 'Files',
  folders: 'Folders',
  size: 'Size',
  name: 'Name',
  share: 'Share',
  elapsed: 'Elapsed',
  unreadable: 'Unreadable',

  moreItems: '…and {count} more items',
  accessDenied: 'Could not read part of this folder',
  emptyFolder: 'This folder is empty.',
  readOnlyNote: 'Read-only: Čysta Dyska never changes or deletes anything.',
  logicalSizeNote: 'Sizes are logical file sizes; on-disk size comes in a later version.',
  sortBy: 'Sort by {column}',
  expand: 'Expand {name}',
  collapse: 'Collapse {name}',
} as const

export type Messages = { [K in keyof typeof en]: string }
