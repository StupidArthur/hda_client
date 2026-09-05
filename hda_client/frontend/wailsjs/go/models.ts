export namespace hda {
	
	export class QueryConfig {
	    url: string;
	    ns: number;
	    tags: string[];
	    end_time: string;
	    duration_sec: number;
	    concurrency: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.ns = source["ns"];
	        this.tags = source["tags"];
	        this.end_time = source["end_time"];
	        this.duration_sec = source["duration_sec"];
	        this.concurrency = source["concurrency"];
	    }
	}
	export class ServerTag {
	    node_id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerTag(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node_id = source["node_id"];
	        this.name = source["name"];
	    }
	}

}

