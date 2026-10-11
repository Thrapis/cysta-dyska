export namespace app {
	
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

}

export namespace attribute {
	
	export class Suggestion {
	    text: Record<string, string>;
	    command?: string;
	    settings?: string;
	
	    static createFrom(source: any = {}) {
	        return new Suggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.command = source["command"];
	        this.settings = source["settings"];
	    }
	}

}

