export namespace main {
	
	export class DatasetSummary {
	    name: string;
	    kind: string;
	    rows: number;
	    tags: string[];
	    tagCount: number;
	    statusColumns: number;
	    hdaValueColumns: number;
	    hdaStatusColumns: number;
	    periodMs: number;
	
	    static createFrom(source: any = {}) {
	        return new DatasetSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.rows = source["rows"];
	        this.tags = source["tags"];
	        this.tagCount = source["tagCount"];
	        this.statusColumns = source["statusColumns"];
	        this.hdaValueColumns = source["hdaValueColumns"];
	        this.hdaStatusColumns = source["hdaStatusColumns"];
	        this.periodMs = source["periodMs"];
	    }
	}
	export class PlaybackFileState {
	    name: string;
	    periodMs: number;
	    currentRow: number;
	    totalRows: number;
	    // Go type: time
	    lastCommit: any;
	    lastCommitMs: number;
	    status: string;
	    lastError: string;
	
	    static createFrom(source: any = {}) {
	        return new PlaybackFileState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.periodMs = source["periodMs"];
	        this.currentRow = source["currentRow"];
	        this.totalRows = source["totalRows"];
	        this.lastCommit = this.convertValues(source["lastCommit"], null);
	        this.lastCommitMs = source["lastCommitMs"];
	        this.status = source["status"];
	        this.lastError = source["lastError"];
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
	export class PresetSummary {
	    root: string;
	    configPath: string;
	    configYaml: string;
	    endpoint: string;
	    namespace: string;
	    files: DatasetSummary[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new PresetSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.configPath = source["configPath"];
	        this.configYaml = source["configYaml"];
	        this.endpoint = source["endpoint"];
	        this.namespace = source["namespace"];
	        this.files = this.convertValues(source["files"], DatasetSummary);
	        this.error = source["error"];
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
	export class RuntimeSnapshot {
	    phase: string;
	    message: string;
	    presetRoot: string;
	    endpoint: string;
	    // Go type: time
	    startedAt: any;
	    hdaFilesDone: number;
	    hdaFilesTotal: number;
	    hdaSkipped: number;
	    hdaSamples: number;
	    hdaElapsedMs: number;
	    importFile: string;
	    tagCount: number;
	    goodCount: number;
	    uncertainCount: number;
	    badCount: number;
	    waitingCount: number;
	    daTagCount: number;
	    playback: PlaybackFileState[];
	
	    static createFrom(source: any = {}) {
	        return new RuntimeSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.presetRoot = source["presetRoot"];
	        this.endpoint = source["endpoint"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.hdaFilesDone = source["hdaFilesDone"];
	        this.hdaFilesTotal = source["hdaFilesTotal"];
	        this.hdaSkipped = source["hdaSkipped"];
	        this.hdaSamples = source["hdaSamples"];
	        this.hdaElapsedMs = source["hdaElapsedMs"];
	        this.importFile = source["importFile"];
	        this.tagCount = source["tagCount"];
	        this.goodCount = source["goodCount"];
	        this.uncertainCount = source["uncertainCount"];
	        this.badCount = source["badCount"];
	        this.waitingCount = source["waitingCount"];
	        this.daTagCount = source["daTagCount"];
	        this.playback = this.convertValues(source["playback"], PlaybackFileState);
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
	export class TagSnapshot {
	    name: string;
	    daValue?: number;
	    daQuality: number;
	    // Go type: time
	    daTime: any;
	    daState: string;
	    hdaValue?: number;
	    hdaQuality: number;
	    // Go type: time
	    hdaTime: any;
	    hdaState: string;
	
	    static createFrom(source: any = {}) {
	        return new TagSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.daValue = source["daValue"];
	        this.daQuality = source["daQuality"];
	        this.daTime = this.convertValues(source["daTime"], null);
	        this.daState = source["daState"];
	        this.hdaValue = source["hdaValue"];
	        this.hdaQuality = source["hdaQuality"];
	        this.hdaTime = this.convertValues(source["hdaTime"], null);
	        this.hdaState = source["hdaState"];
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

