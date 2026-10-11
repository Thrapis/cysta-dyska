import { useEffect, useState } from 'react'
import { AlertTriangle, ArrowDown, ArrowUp, ChevronRight, File, Folder, Link2 } from 'lucide-react'
import { Children } from '../../../wailsjs/go/app/App'
import type { app } from '../../../wailsjs/go/models'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useI18n, type MessageKey } from '@/i18n'
import { formatBytes } from '@/lib/format'
import { cn } from 'cn'

type SortKey = 'size' | 'name' | 'files'
type Sort = { key: SortKey; desc: boolean }

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function sortNodes(nodes: app.NodeDTO[], sort: Sort): app.NodeDTO[] {
  // The "…and N more" summary row always stays last.
  const rest = nodes.filter((n) => n.isRest)
  const items = nodes.filter((n) => !n.isRest)
  items.sort((a, b) => {
    const d =
      sort.key === 'name'
        ? collator.compare(a.name, b.name)
        : sort.key === 'files'
          ? a.files - b.files
          : a.alloc - b.alloc || a.size - b.size
    return sort.desc ? -d : d
  })
  return [...items, ...rest]
}

/** Lazily expandable tree of a finished scan. Children are fetched per level. */
export function SizeTree({ root, scanId }: { root: app.NodeDTO; scanId: number }) {
  const { t } = useI18n()
  const [sort, setSort] = useState<Sort>({ key: 'size', desc: true })

  const header = (key: SortKey, label: MessageKey, className?: string) => {
    const active = sort.key === key
    const Icon = sort.desc ? ArrowDown : ArrowUp
    return (
      <TableHead className={className} aria-sort={active ? (sort.desc ? 'descending' : 'ascending') : 'none'}>
        <button
          type="button"
          className="inline-flex items-center gap-1 hover:text-foreground"
          title={t('sortBy', { column: t(label) })}
          onClick={() => setSort(active ? { key, desc: !sort.desc } : { key, desc: key !== 'name' })}
        >
          {t(label)}
          {active && <Icon className="size-3.5" />}
        </button>
      </TableHead>
    )
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          {header('name', 'name')}
          <TableHead className="w-40">{t('share')}</TableHead>
          {header('size', 'size', 'w-28 text-right')}
          {header('files', 'files', 'w-28 text-right')}
        </TableRow>
      </TableHeader>
      <TableBody>
        <Level key={scanId} parent={root} depth={0} sort={sort} />
      </TableBody>
    </Table>
  )
}

function Level({ parent, depth, sort }: { parent: app.NodeDTO; depth: number; sort: Sort }) {
  const { t } = useI18n()
  const [nodes, setNodes] = useState<app.NodeDTO[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    Children(parent.path)
      .then((kids) => live && setNodes(kids))
      .catch((e) => live && setError(String(e)))
    return () => {
      live = false
    }
  }, [parent.path])

  if (error) {
    return (
      <TableRow>
        <TableCell colSpan={4} className="text-destructive" style={{ paddingLeft: indent(depth) }}>
          {error}
        </TableCell>
      </TableRow>
    )
  }
  if (!nodes) return null
  if (nodes.length === 0 && depth === 0) {
    return (
      <TableRow>
        <TableCell colSpan={4} className="text-muted-foreground">
          {t('emptyFolder')}
        </TableCell>
      </TableRow>
    )
  }

  return (
    <>
      {sortNodes(nodes, sort).map((n) => (
        <Row key={n.isRest ? '__rest__' : n.path} node={n} parent={parent} depth={depth} sort={sort} />
      ))}
    </>
  )
}

function Row({ node, parent, depth, sort }: { node: app.NodeDTO; parent: app.NodeDTO; depth: number; sort: Sort }) {
  const { t, locale, formatNumber } = useI18n()
  const [open, setOpen] = useState(false)
  const share = parent.alloc > 0 ? node.alloc / parent.alloc : 0
  const expandable = node.isDir && node.hasChildren && !node.link

  const label = node.isRest ? t('moreItems', { count: formatNumber(node.files + node.dirs) }) : node.name
  const Icon = node.link ? Link2 : node.isDir ? Folder : File

  return (
    <>
      <TableRow className={cn(node.isRest && 'text-muted-foreground italic')}>
        <TableCell className="max-w-0" style={{ paddingLeft: indent(depth) }}>
          <div className="flex min-w-0 items-center gap-1.5">
            {expandable ? (
              <button
                type="button"
                className="rounded p-0.5 hover:bg-muted"
                aria-expanded={open}
                aria-label={t(open ? 'collapse' : 'expand', { name: node.name })}
                onClick={() => setOpen(!open)}
              >
                <ChevronRight className={cn('size-4 transition-transform', open && 'rotate-90')} />
              </button>
            ) : (
              <span className="w-5 shrink-0" />
            )}
            {!node.isRest && <Icon className="size-4 shrink-0 text-muted-foreground" />}
            <span className="truncate" title={node.isRest ? undefined : node.path}>
              {label}
            </span>
            {node.link && (
              <Badge variant="outline" title={t('linkTitle')}>
                {t('link')}
              </Badge>
            )}
            {node.hardlink && (
              <Badge variant="outline" title={t('hardlinkTitle')}>
                {t('hardlink')}
              </Badge>
            )}
            {node.err && (
              <span title={`${t('accessDenied')}: ${node.err}`}>
                <AlertTriangle className="size-4 shrink-0 text-amber-500" />
              </span>
            )}
          </div>
        </TableCell>
        <TableCell>
          <div className="h-2 w-full overflow-hidden rounded bg-muted" aria-hidden>
            <div className="h-full rounded bg-primary/70" style={{ width: `${(share * 100).toFixed(1)}%` }} />
          </div>
        </TableCell>
        <TableCell
          className="text-right tabular-nums"
          title={t('logicalSize', { size: formatBytes(node.size, locale) })}
        >
          {formatBytes(node.alloc, locale)}
        </TableCell>
        <TableCell className="text-right tabular-nums text-muted-foreground">{formatNumber(node.files)}</TableCell>
      </TableRow>
      {open && <Level parent={node} depth={depth + 1} sort={sort} />}
    </>
  )
}

function indent(depth: number): string {
  return `${0.5 + depth * 1.25}rem`
}
