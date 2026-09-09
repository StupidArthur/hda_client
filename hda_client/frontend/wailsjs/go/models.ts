export namespace hda {
	
	export class AnomalyRow {
	    kind: string;
	    node: string;
	    start: string;
	    end: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new AnomalyRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.node = source["node"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.count = source["count"];
	    }
	}
	export class AnomalyPage {
	    rows: AnomalyRow[];
	    total: number;
	    bad: number;
	    uncertain: number;
	    good_empty: number;
	    anomaly_records: number;
	
	    static createFrom(source: any = {}) {
	        return new AnomalyPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], AnomalyRow);
	        this.total = source["total"];
	        this.bad = source["bad"];
	        this.uncertain = source["uncertain"];
	        this.good_empty = source["good_empty"];
	        this.anomaly_records = source["anomaly_records"];
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
	
	export class AppSettings {
	    url: string;
	    ns: number;
	    mode: string;
	    direct: string;
	    expression: string;
	    csv_name: string;
	    csv_nodes: string[];
	    end_time: string;
	    duration_sec: number;
	    page_size: number;
	    output: string;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.ns = source["ns"];
	        this.mode = source["mode"];
	        this.direct = source["direct"];
	        this.expression = source["expression"];
	        this.csv_name = source["csv_name"];
	        this.csv_nodes = source["csv_nodes"];
	        this.end_time = source["end_time"];
	        this.duration_sec = source["duration_sec"];
	        this.page_size = source["page_size"];
	        this.output = source["output"];
	    }
	}
	export class ParquetViewRow {
	    timestamp: string;
	    node: string;
	    value: number;
	    quality: string;
	    has_value: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ParquetViewRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.node = source["node"];
	        this.value = source["value"];
	        this.quality = source["quality"];
	        this.has_value = source["has_value"];
	    }
	}
	export class ParquetPage {
	    rows: ParquetViewRow[];
	    total: number;
	    nodes: string[];
	
	    static createFrom(source: any = {}) {
	        return new ParquetPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = this.convertValues(source["rows"], ParquetViewRow);
	        this.total = source["total"];
	        this.nodes = source["nodes"];
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
	export class ParquetSummary {
	    records: number;
	    good: number;
	    bad: number;
	    uncertain: number;
	    no_value: number;
	    start: string;
	    end: string;
	    nodes: string[];
	
	    static createFrom(source: any = {}) {
	        return new ParquetSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.records = source["records"];
	        this.good = source["good"];
	        this.bad = source["bad"];
	        this.uncertain = source["uncertain"];
	        this.no_value = source["no_value"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.nodes = source["nodes"];
	    }
	}
	
	export class QueryConfig {
	    url: string;
	    ns: number;
	    tags: string[];
	    end_time: string;
	    duration_sec: number;
	    page_size: number;
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
	        this.page_size = source["page_size"];
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
	export class TagExpressionPreview {
	    count: number;
	    first: string;
	    second: string;
	    last: string;
	
	    static createFrom(source: any = {}) {
	        return new TagExpressionPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.first = source["first"];
	        this.second = source["second"];
	        this.last = source["last"];
	    }
	}
	export class TrendPoint {
	    timestamp: string;
	    value: number;
	    min: number;
	    max: number;
	
	    static createFrom(source: any = {}) {
	        return new TrendPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.value = source["value"];
	        this.min = source["min"];
	        this.max = source["max"];
	    }
	}
	export class TrendSeries {
	    node: string;
	    points: TrendPoint[];
	
	    static createFrom(source: any = {}) {
	        return new TrendSeries(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = source["node"];
	        this.points = this.convertValues(source["points"], TrendPoint);
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

