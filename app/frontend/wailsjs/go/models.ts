export namespace analysis {
	
	export class ContextPoint {
	    Seq: number;
	    Offset: number;
	    Context: number;
	
	    static createFrom(source: any = {}) {
	        return new ContextPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Seq = source["Seq"];
	        this.Offset = source["Offset"];
	        this.Context = source["Context"];
	    }
	}
	export class Ref {
	    Seq: number;
	    Offset: number;
	    Label: string;
	
	    static createFrom(source: any = {}) {
	        return new Ref(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Seq = source["Seq"];
	        this.Offset = source["Offset"];
	        this.Label = source["Label"];
	    }
	}
	export class Repeat {
	    Key: string;
	    Count: number;
	    Seqs: number[];
	
	    static createFrom(source: any = {}) {
	        return new Repeat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Count = source["Count"];
	        this.Seqs = source["Seqs"];
	    }
	}
	export class FileStats {
	    UniqueRead: number;
	    Reads: number;
	    ReadBeforeFirstEdit: number;
	    Edited: string[];
	    Directories: string[];
	    RedundantReads: Repeat[];
	    BroadSearches: Ref[];
	
	    static createFrom(source: any = {}) {
	        return new FileStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.UniqueRead = source["UniqueRead"];
	        this.Reads = source["Reads"];
	        this.ReadBeforeFirstEdit = source["ReadBeforeFirstEdit"];
	        this.Edited = source["Edited"];
	        this.Directories = source["Directories"];
	        this.RedundantReads = this.convertValues(source["RedundantReads"], Repeat);
	        this.BroadSearches = this.convertValues(source["BroadSearches"], Ref);
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
	export class MCPCall {
	    Seq: number;
	    Offset: number;
	    Label: string;
	    Server: string;
	    Tool: string;
	    BeforeEdit: boolean;
	    Relevance: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Seq = source["Seq"];
	        this.Offset = source["Offset"];
	        this.Label = source["Label"];
	        this.Server = source["Server"];
	        this.Tool = source["Tool"];
	        this.BeforeEdit = source["BeforeEdit"];
	        this.Relevance = source["Relevance"];
	    }
	}
	export class ToolCount {
	    Name: string;
	    Calls: number;
	    Failed: number;
	    Duration: number;
	    ResultTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new ToolCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Calls = source["Calls"];
	        this.Failed = source["Failed"];
	        this.Duration = source["Duration"];
	        this.ResultTokens = source["ResultTokens"];
	    }
	}
	export class MCPStats {
	    Calls: number;
	    BeforeFirstEdit: number;
	    ByTool: ToolCount[];
	    Repeated: Repeat[];
	    Refs: MCPCall[];
	
	    static createFrom(source: any = {}) {
	        return new MCPStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Calls = source["Calls"];
	        this.BeforeFirstEdit = source["BeforeFirstEdit"];
	        this.ByTool = this.convertValues(source["ByTool"], ToolCount);
	        this.Repeated = this.convertValues(source["Repeated"], Repeat);
	        this.Refs = this.convertValues(source["Refs"], MCPCall);
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
	
	
	export class TimelineEntry {
	    Seq: number;
	    Offset: number;
	    Agent: string;
	    Kind: string;
	    Category: string;
	    Label: string;
	    IsError: boolean;
	    Duration: number;
	    Context: number;
	
	    static createFrom(source: any = {}) {
	        return new TimelineEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Seq = source["Seq"];
	        this.Offset = source["Offset"];
	        this.Agent = source["Agent"];
	        this.Kind = source["Kind"];
	        this.Category = source["Category"];
	        this.Label = source["Label"];
	        this.IsError = source["IsError"];
	        this.Duration = source["Duration"];
	        this.Context = source["Context"];
	    }
	}
	export class TestStats {
	    Runs: number;
	    Failed: number;
	    Repeated: number;
	    RunRefs: Ref[];
	    FirstEditToFirstTest: number;
	    Style: string;
	
	    static createFrom(source: any = {}) {
	        return new TestStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Runs = source["Runs"];
	        this.Failed = source["Failed"];
	        this.Repeated = source["Repeated"];
	        this.RunRefs = this.convertValues(source["RunRefs"], Ref);
	        this.FirstEditToFirstTest = source["FirstEditToFirstTest"];
	        this.Style = source["Style"];
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
	export class ToolStats {
	    Calls: number;
	    Failed: number;
	    ByName: ToolCount[];
	    ByCategory: Record<string, number>;
	    Repeated: Repeat[];
	    LongRunning: Ref[];
	
	    static createFrom(source: any = {}) {
	        return new ToolStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Calls = source["Calls"];
	        this.Failed = source["Failed"];
	        this.ByName = this.convertValues(source["ByName"], ToolCount);
	        this.ByCategory = source["ByCategory"];
	        this.Repeated = this.convertValues(source["Repeated"], Repeat);
	        this.LongRunning = this.convertValues(source["LongRunning"], Ref);
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
	export class TurnTokens {
	    Turn: number;
	    Seq: number;
	    Prompt: string;
	    ModelCalls: number;
	    Output: number;
	    Processed: number;
	
	    static createFrom(source: any = {}) {
	        return new TurnTokens(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Turn = source["Turn"];
	        this.Seq = source["Seq"];
	        this.Prompt = source["Prompt"];
	        this.ModelCalls = source["ModelCalls"];
	        this.Output = source["Output"];
	        this.Processed = source["Processed"];
	    }
	}
	export class TokenStats {
	    ModelCalls: number;
	    Input: number;
	    CacheCreation: number;
	    CacheRead: number;
	    Output: number;
	    Processed: number;
	    PeakContext: number;
	    ByTurn: TurnTokens[];
	    Growth: ContextPoint[];
	    BeforeFirstEdit: number;
	
	    static createFrom(source: any = {}) {
	        return new TokenStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ModelCalls = source["ModelCalls"];
	        this.Input = source["Input"];
	        this.CacheCreation = source["CacheCreation"];
	        this.CacheRead = source["CacheRead"];
	        this.Output = source["Output"];
	        this.Processed = source["Processed"];
	        this.PeakContext = source["PeakContext"];
	        this.ByTurn = this.convertValues(source["ByTurn"], TurnTokens);
	        this.Growth = this.convertValues(source["Growth"], ContextPoint);
	        this.BeforeFirstEdit = source["BeforeFirstEdit"];
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
	export class Report {
	    SessionID: string;
	    Duration: number;
	    Turns: number;
	    Tokens: TokenStats;
	    Tools: ToolStats;
	    Files: FileStats;
	    Tests: TestStats;
	    MCP: MCPStats;
	    Subagents: Ref[];
	    Skills: Ref[];
	    Failures: Ref[];
	    Timeline: TimelineEntry[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.SessionID = source["SessionID"];
	        this.Duration = source["Duration"];
	        this.Turns = source["Turns"];
	        this.Tokens = this.convertValues(source["Tokens"], TokenStats);
	        this.Tools = this.convertValues(source["Tools"], ToolStats);
	        this.Files = this.convertValues(source["Files"], FileStats);
	        this.Tests = this.convertValues(source["Tests"], TestStats);
	        this.MCP = this.convertValues(source["MCP"], MCPStats);
	        this.Subagents = this.convertValues(source["Subagents"], Ref);
	        this.Skills = this.convertValues(source["Skills"], Ref);
	        this.Failures = this.convertValues(source["Failures"], Ref);
	        this.Timeline = this.convertValues(source["Timeline"], TimelineEntry);
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

export namespace insight {
	
	export class SessionRef {
	    session: string;
	    seq: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = source["session"];
	        this.seq = source["seq"];
	    }
	}
	export class Finding {
	    rule: string;
	    target: string;
	    title: string;
	    observation: string;
	    hypothesis: string;
	    suggestion: string;
	    snippet: string;
	    benefit: string;
	    downside: string;
	    confidence: string;
	    tokensInvolved: number;
	    evidence: number[];
	    sessions?: SessionRef[];
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = source["rule"];
	        this.target = source["target"];
	        this.title = source["title"];
	        this.observation = source["observation"];
	        this.hypothesis = source["hypothesis"];
	        this.suggestion = source["suggestion"];
	        this.snippet = source["snippet"];
	        this.benefit = source["benefit"];
	        this.downside = source["downside"];
	        this.confidence = source["confidence"];
	        this.tokensInvolved = source["tokensInvolved"];
	        this.evidence = source["evidence"];
	        this.sessions = this.convertValues(source["sessions"], SessionRef);
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

export namespace main {
	
	export class Candidate {
	    path: string;
	    id: string;
	    project: string;
	    firstPrompt: string;
	    modifiedAt: string;
	    sizeBytes: number;
	    imported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Candidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.id = source["id"];
	        this.project = source["project"];
	        this.firstPrompt = source["firstPrompt"];
	        this.modifiedAt = source["modifiedAt"];
	        this.sizeBytes = source["sizeBytes"];
	        this.imported = source["imported"];
	    }
	}
	export class EventDetail {
	    seq: number;
	    kind: string;
	    agent: string;
	    timestamp: string;
	    text: string;
	    toolName: string;
	    input: string;
	    output: string;
	    outputBytes: number;
	    isError: boolean;
	    durationNs: number;
	
	    static createFrom(source: any = {}) {
	        return new EventDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.kind = source["kind"];
	        this.agent = source["agent"];
	        this.timestamp = source["timestamp"];
	        this.text = source["text"];
	        this.toolName = source["toolName"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.outputBytes = source["outputBytes"];
	        this.isError = source["isError"];
	        this.durationNs = source["durationNs"];
	    }
	}
	export class RepoFindings {
	    path: string;
	    name: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new RepoFindings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.count = source["count"];
	    }
	}
	export class GlobalView {
	    findings: insight.Finding[];
	    repos: RepoFindings[];
	
	    static createFrom(source: any = {}) {
	        return new GlobalView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.findings = this.convertValues(source["findings"], insight.Finding);
	        this.repos = this.convertValues(source["repos"], RepoFindings);
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
	export class PromptRow {
	    sessionId: string;
	    seq: number;
	    at: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new PromptRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.seq = source["seq"];
	        this.at = source["at"];
	        this.text = source["text"];
	    }
	}
	
	export class RepoRow {
	    path: string;
	    name: string;
	    sessions: number;
	    lastAt: string;
	    tokens: number;
	
	    static createFrom(source: any = {}) {
	        return new RepoRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.sessions = source["sessions"];
	        this.lastAt = source["lastAt"];
	        this.tokens = source["tokens"];
	    }
	}
	export class SessionRow {
	    id: string;
	    project: string;
	    startedAt: string;
	    durationNs: number;
	    events: number;
	    prompts: number;
	    tokens: number;
	    peak: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.project = source["project"];
	        this.startedAt = source["startedAt"];
	        this.durationNs = source["durationNs"];
	        this.events = source["events"];
	        this.prompts = source["prompts"];
	        this.tokens = source["tokens"];
	        this.peak = source["peak"];
	    }
	}
	export class RepoView {
	    path: string;
	    name: string;
	    sessions: SessionRow[];
	    prompts: PromptRow[];
	    findings: insight.Finding[];
	
	    static createFrom(source: any = {}) {
	        return new RepoView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.sessions = this.convertValues(source["sessions"], SessionRow);
	        this.prompts = this.convertValues(source["prompts"], PromptRow);
	        this.findings = this.convertValues(source["findings"], insight.Finding);
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
	
	export class SessionView {
	    id: string;
	    project: string;
	    sourcePath: string;
	    startedAt: string;
	    warnings: string[];
	    report?: analysis.Report;
	    findings: insight.Finding[];
	
	    static createFrom(source: any = {}) {
	        return new SessionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.project = source["project"];
	        this.sourcePath = source["sourcePath"];
	        this.startedAt = source["startedAt"];
	        this.warnings = source["warnings"];
	        this.report = this.convertValues(source["report"], analysis.Report);
	        this.findings = this.convertValues(source["findings"], insight.Finding);
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

