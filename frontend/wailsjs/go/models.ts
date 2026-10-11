export namespace app {
	
	export class ActionLogDTO {
	    id: number;
	    at: number;
	    kind: string;
	    owner: string;
	    target: string;
	    command: string;
	    bytesBefore: number;
	    bytesAfter: number;
	    outcome: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new ActionLogDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.at = source["at"];
	        this.kind = source["kind"];
	        this.owner = source["owner"];
	        this.target = source["target"];
	        this.command = source["command"];
	        this.bytesBefore = source["bytesBefore"];
	        this.bytesAfter = source["bytesAfter"];
	        this.outcome = source["outcome"];
	        this.detail = source["detail"];
	    }
	}
	export class ActionOptionDTO {
	    kind: string;
	    suggest: number;
	    command: string;
	    needsAck: boolean;
	    blocked: string;
	
	    static createFrom(source: any = {}) {
	        return new ActionOptionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.suggest = source["suggest"];
	        this.command = source["command"];
	        this.needsAck = source["needsAck"];
	        this.blocked = source["blocked"];
	    }
	}
	export class ActionRequest {
	    ruleId: string;
	    path: string;
	    kind: string;
	    suggest: number;
	
	    static createFrom(source: any = {}) {
	        return new ActionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ruleId = source["ruleId"];
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.suggest = source["suggest"];
	    }
	}
	export class ActionResultDTO {
	    outcome: string;
	    freed: number;
	    output: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new ActionResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.freed = source["freed"];
	        this.output = source["output"];
	        this.detail = source["detail"];
	    }
	}
	export class CulpritDTO {
	    ruleId: string;
	    owner: string;
	    category: string;
	    risk: string;
	    path: string;
	    alloc: number;
	    unreadable: boolean;
	    explain: Record<string, string>;
	    suggest: attribute.Suggestion[];
	    actions: ActionOptionDTO[];
	
	    static createFrom(source: any = {}) {
	        return new CulpritDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ruleId = source["ruleId"];
	        this.owner = source["owner"];
	        this.category = source["category"];
	        this.risk = source["risk"];
	        this.path = source["path"];
	        this.alloc = source["alloc"];
	        this.unreadable = source["unreadable"];
	        this.explain = source["explain"];
	        this.suggest = this.convertValues(source["suggest"], attribute.Suggestion);
	        this.actions = this.convertValues(source["actions"], ActionOptionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CulpritsDTO {
	    items: CulpritDTO[];
	    omitted: number;
	    explained: number;
	    scanAlloc: number;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new CulpritsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], CulpritDTO);
	        this.omitted = source["omitted"];
	        this.explained = source["explained"];
	        this.scanAlloc = source["scanAlloc"];
	        this.warning = source["warning"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiffRowDTO {
	    key: string;
	    path: string;
	    isDir: boolean;
	    before: number;
	    after: number;
	    delta: number;
	    beforeKnown: boolean;
	    afterKnown: boolean;
	    hasChildren: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiffRowDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.delta = source["delta"];
	        this.beforeKnown = source["beforeKnown"];
	        this.afterKnown = source["afterKnown"];
	        this.hasChildren = source["hasChildren"];
	    }
	}
	export class SnapshotDTO {
	    id: number;
	    takenAt: number;
	    alloc: number;
	    files: number;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.takenAt = source["takenAt"];
	        this.alloc = source["alloc"];
	        this.files = source["files"];
	        this.durationMs = source["durationMs"];
	    }
	}
	export class HistoryDTO {
	    snapshots: SnapshotDTO[];
	    method: string;
	    currentId: number;
	    minAlloc: number;
	    hotspotMin: number;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshots = this.convertValues(source["snapshots"], SnapshotDTO);
	        this.method = source["method"];
	        this.currentId = source["currentId"];
	        this.minAlloc = source["minAlloc"];
	        this.hotspotMin = source["hotspotMin"];
	        this.warning = source["warning"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NodeDTO {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    alloc: number;
	    files: number;
	    dirs: number;
	    err?: string;
	    link: boolean;
	    hardlink: boolean;
	    hasChildren: boolean;
	    isRest: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NodeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.alloc = source["alloc"];
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	        this.err = source["err"];
	        this.link = source["link"];
	        this.hardlink = source["hardlink"];
	        this.hasChildren = source["hasChildren"];
	        this.isRest = source["isRest"];
	    }
	}
	export class PlanDTO {
	    id: string;
	    kind: string;
	    display: string;
	    targets: string[];
	    targetCount: number;
	    bytes: number;
	    files: number;
	    needsAck: boolean;
	    blocked: string;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new PlanDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.display = source["display"];
	        this.targets = source["targets"];
	        this.targetCount = source["targetCount"];
	        this.bytes = source["bytes"];
	        this.files = source["files"];
	        this.needsAck = source["needsAck"];
	        this.blocked = source["blocked"];
	        this.warnings = source["warnings"];
	    }
	}
	export class SettingsDTO {
	    allowPermanentDelete: boolean;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allowPermanentDelete = source["allowPermanentDelete"];
	        this.available = source["available"];
	    }
	}
	
	export class TileDTO {
	    name: string;
	    path: string;
	    isDir: boolean;
	    alloc: number;
	    hasChildren: boolean;
	    isRest: boolean;
	    count: number;
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new TileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.alloc = source["alloc"];
	        this.hasChildren = source["hasChildren"];
	        this.isRest = source["isRest"];
	        this.count = source["count"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}

}

export namespace attribute {
	
	export class Run {
	    program: string;
	    args?: string[];
	    admin?: boolean;
	    in?: string;
	
	    static createFrom(source: any = {}) {
	        return new Run(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.program = source["program"];
	        this.args = source["args"];
	        this.admin = source["admin"];
	        this.in = source["in"];
	    }
	}
	export class Suggestion {
	    text: Record<string, string>;
	    command?: string;
	    settings?: string;
	    run?: Run;
	
	    static createFrom(source: any = {}) {
	        return new Suggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.command = source["command"];
	        this.settings = source["settings"];
	        this.run = this.convertValues(source["run"], Run);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

