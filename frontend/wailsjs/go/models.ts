export namespace app {
	
	export class NodeDTO {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    files: number;
	    dirs: number;
	    err?: string;
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
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	        this.err = source["err"];
	        this.hasChildren = source["hasChildren"];
	        this.isRest = source["isRest"];
	    }
	}

}

