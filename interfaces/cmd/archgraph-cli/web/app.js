// ArchGraph Web Dashboard Controller
let config = {
    storage: "http://localhost:8080",
    serving: "http://localhost:8081",
    namespace: "acme"
};

// Graph State
let allEntities = [];
let allRelationships = [];
let filteredNodes = [];
let filteredLinks = [];

// Log & Replay State
let logEntries = [];
let currentLogIndex = -1;
let gitCommits = [];

// Selected & Highlight States
let selectedNode = null;
let hoveredNode = null;
let blastRadiusNodes = new Set();
let blastRadiusLinks = new Set();
let isBlastRadiusActive = false;

// UI Panels
let activeTab = 'inspector'; // inspector | ask

// Demo mode: render the wiki from a static fixture (no backend required).
// Enable with ?demo=1 — used for design review / headless screenshots.
const DEMO = new URLSearchParams(location.search).get("demo") === "1";

// D3 Force Simulation Setup
const canvas = document.getElementById("graph-canvas");
const ctx = canvas.getContext("2d");
let width, height;
let simulation;
let transform = d3.zoomIdentity;

// Resize Handler
function resize() {
    const parent = canvas.parentElement;
    width = parent.clientWidth;
    height = parent.clientHeight;
    canvas.width = width * window.devicePixelRatio;
    canvas.height = height * window.devicePixelRatio;
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";
    ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
    if (simulation) {
        simulation.force("center", d3.forceCenter(width / 2, height / 2));
        simulation.alpha(0.3).restart();
    }
}
window.addEventListener("resize", resize);
resize();

// Load Git commits for timeline context
async function loadGitCommits() {
    try {
        const resp = await fetch(`/api/commits?namespace=${encodeURIComponent(config.namespace)}`);
        if (resp.ok) {
            gitCommits = await resp.json();
            console.log("Loaded Git commits:", gitCommits);
        } else {
            gitCommits = [];
        }
    } catch (e) {
        console.warn("Could not load Git commits:", e);
        gitCommits = [];
    }
}

// Load Config & Bootstrap
async function init() {
    try {
        const resp = await fetch("/api/config");
        if (resp.ok) {
            config = await resp.json();
            console.log("Loaded ArchGraph configurations:", config);
        }
    } catch (e) {
        console.warn("Could not load config from server, using defaults", e);
    }

    document.getElementById("stat-namespace").textContent = config.namespace;
    
    // Initialize UI event handlers
    setupEventHandlers();
    setupThemeToggle();
    renderSuggestions();
    
    // Setup D3 zoom
    d3.select(canvas).call(
        d3.zoom()
            .scaleExtent([0.1, 8])
            .on("zoom", (event) => {
                transform = event.transform;
                ticked();
            })
    );

    // Hash-based view switching
    let initialView = "home";
    if (window.location.hash === "#wiki") {
        initialView = "wiki";
    } else if (window.location.hash === "#graph") {
        initialView = "graph";
    }

    if (DEMO) initialView = "wiki";

    if (initialView !== "home" && !DEMO) {
        await loadGitCommits();
        await refreshGraphData();
        await loadTransactionLogs();
    }
    switchView(initialView);
}

// Fetch main graph data and health smells
async function refreshGraphData() {
    try {
        await loadGitCommits();
        const u = `${config.serving}/v1/entities?namespace=${encodeURIComponent(config.namespace)}`;
        const resp = await fetch(u);
        if (!resp.ok) throw new Error("Failed to fetch graph data");
        const data = await resp.json();
        
        allEntities = data.entities || [];
        allRelationships = data.relationships || [];
        
        document.getElementById("stat-entities").textContent = allEntities.length;
        document.getElementById("stat-relationships").textContent = allRelationships.length;

        // Apply filters
        applyFiltersAndRebuild();
        
        // Fetch health smells
        await refreshHealthAudits();
    } catch (e) {
        console.error("Error refreshing graph data:", e);
        document.querySelector(".server-status .indicator").className = "indicator disconnected";
        document.querySelector(".server-status .status-text").textContent = "Connection Refused";
    }
}

async function refreshHealthAudits() {
    try {
        const resp = await fetch(`${config.serving}/v1/health-audit`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ namespace: config.namespace })
        });
        
        const badge = document.getElementById("stat-health-badge");
        const healthText = document.getElementById("stat-health");
        const listContainer = document.getElementById("smells-list");
        
        if (resp.ok) {
            const report = await resp.json();
            const smells = report.smells || [];
            document.getElementById("smells-count").textContent = `${smells.length} architectural smells detected`;

            if (smells.length === 0) {
                badge.className = "stat-badge healthy";
                healthText.textContent = "Healthy";
                listContainer.innerHTML = '<div class="no-smells">✅ Governance checks passing</div>';
            } else {
                badge.className = "stat-badge smells";
                healthText.textContent = "Smells Found";
                
                listContainer.innerHTML = smells.map(smell => {
                    const sevClass = (smell.severity === "FAIL" || smell.severity === "HIGH") ? "high" : "medium";
                    return `
                        <div class="smell-card ${sevClass}" onclick="highlightSmell(${JSON.stringify(smell.node_ids || []).replace(/"/g, '&quot;')})">
                            <div class="smell-header">
                                <span class="type">${smell.type.replace(/_/g, ' ')}</span>
                                <span class="severity">${smell.severity}</span>
                            </div>
                            <div class="message">${smell.message}</div>
                            ${smell.nodes ? `<div class="affected-nodes">Affected: ${smell.nodes.join(', ')}</div>` : ''}
                        </div>
                    `;
                }).join('');
            }
        }
    } catch (e) {
        console.error("Health audit failed:", e);
    }
}

// Fetch historical log for replaying
async function loadTransactionLogs() {
    try {
        const resp = await fetch(`${config.serving}/v1/log?limit=200`);
        if (resp.ok) {
            const data = await resp.json();
            logEntries = data.entries || [];
            setupTimeline();
        }
    } catch (e) {
        console.warn("Log fetch failed:", e);
    }
}

// Filter logic
function applyFiltersAndRebuild() {
    const showServices = document.getElementById("filter-services").checked;
    const showModules = document.getElementById("filter-modules").checked;
    const showFunctions = document.getElementById("filter-functions").checked;
    const showEndpoints = document.getElementById("filter-endpoints").checked;
    const showDatabases = document.getElementById("filter-databases").checked;

    // Filter nodes
    filteredNodes = allEntities.filter(node => {
        if (node.type === "SERVICE" && !showServices) return false;
        if (node.type === "MODULE" && !showModules) return false;
        if (node.type === "FUNCTION" && !showFunctions) return false;
        if (node.type === "API_ENDPOINT" && !showEndpoints) return false;
        if (node.type === "DATABASE_TABLE" && !showDatabases) return false;
        return true;
    });

    const activeNodeIDs = new Set(filteredNodes.map(n => n.id));

    // Filter links
    filteredLinks = allRelationships.filter(rel => {
        return activeNodeIDs.has(rel.from_id) && activeNodeIDs.has(rel.to_id);
    }).map(rel => {
        return {
            id: rel.id,
            type: rel.type,
            source: rel.from_id,
            target: rel.to_id,
            confidence: rel.confidence,
            properties: rel.properties
        };
    });

    // Rebuild D3 simulation
    rebuildSimulation();
}

function rebuildSimulation() {
    if (simulation) simulation.stop();

    simulation = d3.forceSimulation(filteredNodes)
        .force("link", d3.forceLink(filteredLinks).id(d => d.id).distance(120))
        .force("charge", d3.forceManyBody().strength(-220))
        .force("center", d3.forceCenter(width / 2, height / 2))
        .force("collision", d3.forceCollide().radius(40))
        .on("tick", ticked);

    // If node was selected and is now filtered out, clear selection
    if (selectedNode && !filteredNodes.some(n => n.id === selectedNode.id)) {
        clearInspector();
    }
    
    // Warm up the simulation slightly
    for (let i = 0; i < 40; ++i) simulation.tick();
    ticked();
}

// Canvas rendering tick
function ticked() {
    ctx.clearRect(0, 0, width, height);
    ctx.save();
    ctx.translate(transform.x, transform.y);
    ctx.scale(transform.k, transform.k);

    // Draw Links
    filteredLinks.forEach(drawLink);

    // Draw Nodes
    filteredNodes.forEach(drawNode);

    ctx.restore();
}

function getEntityColor(type) {
    switch (type) {
        case "SERVICE": return "#a29bfe";
        case "MODULE": return "#00cec9";
        case "FUNCTION": return "#ffeaa7";
        case "API_ENDPOINT": return "#ff7675";
        case "DATABASE_TABLE":
        case "DATABASE_SCHEMA":
            return "#74b9ff";
        default: return "#9ea0b0";
    }
}

function drawLink(link) {
    const src = typeof link.source === 'object' ? link.source : filteredNodes.find(n => n.id === link.source);
    const tgt = typeof link.target === 'object' ? link.target : filteredNodes.find(n => n.id === link.target);
    if (!src || !tgt) return;

    const isHovered = hoveredNode && (hoveredNode.id === src.id || hoveredNode.id === tgt.id);
    const isBRHighlighted = isBlastRadiusActive && blastRadiusLinks.has(link.id);

    ctx.beginPath();
    ctx.moveTo(src.x, src.y);
    ctx.lineTo(tgt.x, tgt.y);

    if (isBRHighlighted) {
        ctx.strokeStyle = "rgba(255, 82, 82, 0.85)";
        ctx.lineWidth = 2.5;
        ctx.shadowColor = "rgba(255, 82, 82, 0.4)";
        ctx.shadowBlur = 6;
    } else if (isHovered) {
        ctx.strokeStyle = "rgba(255, 255, 255, 0.6)";
        ctx.lineWidth = 1.8;
        ctx.shadowBlur = 0;
    } else {
        ctx.strokeStyle = "rgba(255, 255, 255, 0.08)";
        ctx.lineWidth = 1.0;
        ctx.shadowBlur = 0;
    }
    ctx.stroke();

    // Draw link label (relationship type) under high zoom or hover
    if (transform.k > 1.2 || isHovered || isBRHighlighted) {
        ctx.save();
        ctx.font = "8px 'JetBrains Mono', monospace";
        ctx.fillStyle = isBRHighlighted ? "#ff5252" : "rgba(255, 255, 255, 0.4)";
        ctx.textAlign = "center";
        const midX = (src.x + tgt.x) / 2;
        const midY = (src.y + tgt.y) / 2;
        ctx.fillText(link.type, midX, midY - 3);
        ctx.restore();
    }

    // Draw Arrowhead
    const angle = Math.atan2(tgt.y - src.y, tgt.x - src.x);
    const arrowLength = 7;
    const nodeRadius = 16;
    // Intersection at node edge
    const arrowX = tgt.x - nodeRadius * Math.cos(angle);
    const arrowY = tgt.y - nodeRadius * Math.sin(angle);

    ctx.save();
    ctx.beginPath();
    ctx.moveTo(arrowX, arrowY);
    ctx.lineTo(arrowX - arrowLength * Math.cos(angle - Math.PI / 6), arrowY - arrowLength * Math.sin(angle - Math.PI / 6));
    ctx.lineTo(arrowX - arrowLength * Math.cos(angle + Math.PI / 6), arrowY - arrowLength * Math.sin(angle + Math.PI / 6));
    ctx.closePath();
    ctx.fillStyle = isBRHighlighted ? "#ff5252" : (isHovered ? "rgba(255, 255, 255, 0.6)" : "rgba(255, 255, 255, 0.1)");
    ctx.fill();
    ctx.restore();
}

function drawNode(node) {
    const color = getEntityColor(node.type);
    const isSelected = selectedNode && selectedNode.id === node.id;
    const isHovered = hoveredNode && hoveredNode.id === node.id;
    const isBRHighlighted = isBlastRadiusActive && blastRadiusNodes.has(node.id);
    const isBRDimmed = isBlastRadiusActive && !blastRadiusNodes.has(node.id);

    ctx.save();
    
    // Transparency when blast radius dimmer is running
    if (isBRDimmed) {
        ctx.globalAlpha = 0.2;
    } else {
        ctx.globalAlpha = 1.0;
    }

    // Halo
    if (isSelected) {
        ctx.beginPath();
        ctx.arc(node.x, node.y, 22, 0, 2 * Math.PI);
        ctx.strokeStyle = "rgba(108, 92, 231, 0.6)";
        ctx.lineWidth = 2;
        ctx.stroke();
    } else if (isHovered) {
        ctx.beginPath();
        ctx.arc(node.x, node.y, 20, 0, 2 * Math.PI);
        ctx.strokeStyle = "rgba(255, 255, 255, 0.4)";
        ctx.lineWidth = 1.5;
        ctx.stroke();
    }

    // Node Body
    ctx.beginPath();
    ctx.arc(node.x, node.y, 16, 0, 2 * Math.PI);
    ctx.fillStyle = color;
    
    if (isBRHighlighted) {
        ctx.shadowColor = "#ff5252";
        ctx.shadowBlur = 15;
        ctx.strokeStyle = "#ffffff";
        ctx.lineWidth = 2;
        ctx.stroke();
    } else {
        ctx.shadowColor = color;
        ctx.shadowBlur = isHovered ? 8 : 4;
    }
    ctx.fill();
    ctx.shadowBlur = 0; // reset

    // Label Text
    ctx.fillStyle = isBRHighlighted ? "#ff5252" : "#f0f0f5";
    ctx.font = isSelected ? "bold 11px Outfit, sans-serif" : "10px Outfit, sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    
    // Shorten name if too long
    let displayName = node.canonical_name;
    if (node.type === "MODULE") {
        displayName = node.properties?.package_name || displayName.split("/").pop();
    } else if (node.type === "FUNCTION") {
        const parts = displayName.split("/");
        displayName = parts[parts.length - 1];
    }
    if (displayName.length > 20) {
        displayName = displayName.substring(0, 17) + "...";
    }
    ctx.fillText(displayName, node.x, node.y + 20);

    // Draw tiny icon letter inside node center
    ctx.fillStyle = "#000000";
    ctx.font = "bold 10px monospace";
    ctx.textBaseline = "middle";
    const letter = node.type.charAt(0);
    ctx.fillText(letter, node.x, node.y);

    ctx.restore();
}

// User interactions
function setupEventHandlers() {
    // Zoom control click listeners
    document.getElementById("btn-zoom-in").addEventListener("click", () => {
        d3.select(canvas).transition().duration(250).call(d3.zoom().transform, transform.scale(1.2));
    });
    document.getElementById("btn-zoom-out").addEventListener("click", () => {
        d3.select(canvas).transition().duration(250).call(d3.zoom().transform, transform.scale(0.8));
    });
    document.getElementById("btn-reset").addEventListener("click", () => {
        d3.select(canvas).transition().duration(250).call(d3.zoom().transform, d3.zoomIdentity);
    });

    // Checkbox toggles
    ["filter-services", "filter-modules", "filter-functions", "filter-endpoints", "filter-databases"].forEach(id => {
        document.getElementById(id).addEventListener("change", applyFiltersAndRebuild);
    });

    // Inspector Tabs
    document.getElementById("tab-inspector").addEventListener("click", () => switchTab('inspector'));
    document.getElementById("tab-ask").addEventListener("click", () => switchTab('ask'));

    // Question button
    document.getElementById("btn-send-question").addEventListener("click", sendQuestion);
    document.getElementById("chat-input").addEventListener("keydown", (e) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            sendQuestion();
        }
    });

    // Blast Radius buttons
    document.getElementById("btn-blast-radius").addEventListener("click", simulateBlastRadius);
    document.getElementById("btn-clear-analysis").addEventListener("click", clearBlastRadius);

    // Ingestion Button
    document.getElementById("btn-ingest-repo").addEventListener("click", runRepositoryIngestion);

    // Landing Page Search Handlers
    const searchInput = document.getElementById("search-repo-input");
    const searchBtn = document.getElementById("btn-search-repo");
    if (searchBtn && searchInput) {
        searchBtn.addEventListener("click", handleSearchOrIngest);
        searchInput.addEventListener("keydown", (e) => {
            if (e.key === "Enter") {
                handleSearchOrIngest();
            }
        });
    }

    // Wiki Handlers
    document.getElementById("btn-wiki-send-question").addEventListener("click", () => askWikiAssistant());
    document.getElementById("wiki-chat-input").addEventListener("keydown", (e) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            askWikiAssistant();
        }
    });

    document.getElementById("wiki-ns-input").addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
            const val = e.target.value.trim();
            if (val) {
                config.namespace = val;
                document.getElementById("stat-namespace").textContent = config.namespace;
                refreshGraphData();
                loadWiki();
            }
        }
    });

    // Canvas click & drag delegation
    d3.select(canvas)
        .on("mousemove", handleCanvasMouseMove)
        .on("click", handleCanvasClick)
        .call(d3.drag()
            .container(canvas)
            .subject(dragSubject)
            .on("start", dragStarted)
            .on("drag", dragged)
            .on("end", dragEnded)
        );
}

function switchTab(tab) {
    activeTab = tab;
    document.getElementById("tab-inspector").className = tab === 'inspector' ? 'tab-btn active' : 'tab-btn';
    document.getElementById("tab-ask").className = tab === 'ask' ? 'tab-btn active' : 'tab-btn';
    
    document.getElementById("panel-inspector").className = tab === 'inspector' ? 'tab-content' : 'tab-content hidden';
    document.getElementById("panel-ask").className = tab === 'ask' ? 'tab-content' : 'tab-content hidden';
}

// Find hovered node
function findNodeAt(x, y) {
    const localX = (x - transform.x) / transform.k;
    const localY = (y - transform.y) / transform.k;

    for (let i = filteredNodes.length - 1; i >= 0; i--) {
        const n = filteredNodes[i];
        const dist = Math.hypot(n.x - localX, n.y - localY);
        if (dist <= 20) { // click radius slightly larger than visual circle
            return n;
        }
    }
    return null;
}

function handleCanvasMouseMove(event) {
    const rect = canvas.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;
    
    const node = findNodeAt(x, y);
    if (node !== hoveredNode) {
        hoveredNode = node;
        ticked();
    }
}

function handleCanvasClick(event) {
    const rect = canvas.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;
    
    const node = findNodeAt(x, y);
    if (node) {
        selectedNode = node;
        inspectNode(node);
    } else {
        selectedNode = null;
        clearInspector();
    }
    ticked();
}

// Drag behaviors
function dragSubject(event) {
    const rect = canvas.getBoundingClientRect();
    const x = event.sourceEvent.clientX - rect.left;
    const y = event.sourceEvent.clientY - rect.top;
    return findNodeAt(x, y);
}

function dragStarted(event) {
    if (!event.active) simulation.alphaTarget(0.3).restart();
    event.subject.fx = event.subject.x;
    event.subject.fy = event.subject.y;
}

function dragged(event) {
    const localX = (event.x - transform.x) / transform.k;
    const localY = (event.y - transform.y) / transform.k;
    event.subject.fx = localX;
    event.subject.fy = localY;
}

function dragEnded(event) {
    if (!event.active) simulation.alphaTarget(0);
    event.subject.fx = null;
    event.subject.fy = null;
}

// Inspector logic
function inspectNode(node) {
    document.getElementById("inspector-empty").className = "panel-empty hidden";
    document.getElementById("inspector-details").className = "details-content";

    // Set header
    const badge = document.getElementById("ent-badge");
    badge.textContent = node.type;
    badge.className = `badge ${node.type.toLowerCase().replace('_table', '')}`;
    document.getElementById("ent-name").textContent = node.canonical_name;
    document.getElementById("ent-subtype").textContent = node.sub_type || "generic";

    // Provenance
    document.getElementById("ent-id").textContent = node.id;
    document.getElementById("ent-source-id").textContent = node.source ? node.source.source_id : "N/A";
    document.getElementById("ent-observed").textContent = node.source ? new Date(node.source.observed_at).toLocaleString() : "N/A";
    document.getElementById("ent-confidence").textContent = (node.confidence * 100).toFixed(1) + "%";

    // Custom Properties
    const propBox = document.getElementById("ent-custom-properties");
    propBox.innerHTML = "";
    if (node.properties && Object.keys(node.properties).length > 0) {
        for (const [key, val] of Object.entries(node.properties)) {
            const row = document.createElement("div");
            row.className = "prop-row";
            row.innerHTML = `<span class="prop-key">${key}:</span><span class="prop-val">${JSON.stringify(val)}</span>`;
            propBox.appendChild(row);
        }
    } else {
        propBox.innerHTML = '<span class="subtitle">No extra parameters</span>';
    }

    // Dependencies Listing
    const inbound = allRelationships.filter(r => r.to_id === node.id);
    const outbound = allRelationships.filter(r => r.from_id === node.id);

    document.getElementById("ent-inbound-count").textContent = inbound.length;
    document.getElementById("ent-outbound-count").textContent = outbound.length;

    const depList = document.getElementById("ent-dep-list");
    depList.innerHTML = "";
    
    const combinedDeps = [
        ...inbound.map(r => ({ r, isOut: false, partnerID: r.from_id })),
        ...outbound.map(r => ({ r, isOut: true, partnerID: r.to_id }))
    ];

    if (combinedDeps.length > 0) {
        combinedDeps.forEach(dep => {
            const partner = allEntities.find(e => e.id === dep.partnerID);
            const pName = partner ? partner.canonical_name : dep.partnerID;
            const directionText = dep.isOut ? "➔ calls" : "◀ called by";

            const div = document.createElement("div");
            div.className = "dep-item";
            div.innerHTML = `<span class="target">${pName}</span><span class="type-rel">${directionText} [${dep.r.type}]</span>`;
            depList.appendChild(div);
        });
    } else {
        depList.innerHTML = '<span class="subtitle">No links registered</span>';
    }

    // Toggle button visibility
    if (isBlastRadiusActive) {
        document.getElementById("btn-clear-analysis").className = "action-btn";
    } else {
        document.getElementById("btn-clear-analysis").className = "action-btn hidden";
    }
}

function clearInspector() {
    document.getElementById("inspector-empty").className = "panel-empty";
    document.getElementById("inspector-details").className = "details-content hidden";
}

// Blast Radius Simulator
async function simulateBlastRadius() {
    if (!selectedNode) return;
    
    try {
        const resp = await fetch(`${config.serving}/v1/blast-radius`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                entity_id: selectedNode.id,
                max_depth: 3
            })
        });

        if (resp.ok) {
            const br = await resp.json();
            
            // Set of highlighted elements
            blastRadiusNodes.clear();
            blastRadiusLinks.clear();

            blastRadiusNodes.add(selectedNode.id);
            if (br.affected) {
                br.affected.forEach(aff => {
                    blastRadiusNodes.add(aff.node_id);
                });
            }

            // Find all relationships linking these affected nodes
            allRelationships.forEach(rel => {
                if (blastRadiusNodes.has(rel.from_id) && blastRadiusNodes.has(rel.to_id)) {
                    // Check if they are part of the downstream path from the origin
                    blastRadiusLinks.add(rel.id);
                }
            });

            isBlastRadiusActive = true;
            document.getElementById("btn-clear-analysis").className = "action-btn";
            ticked();
        }
    } catch (e) {
        console.error("Blast radius failed:", e);
    }
}

function clearBlastRadius() {
    isBlastRadiusActive = false;
    blastRadiusNodes.clear();
    blastRadiusLinks.clear();
    document.getElementById("btn-clear-analysis").className = "action-btn hidden";
    ticked();
}

function highlightSmell(nodeIDs) {
    if (!nodeIDs || nodeIDs.length === 0) return;
    
    // Highlight these specific nodes in the graph
    isBlastRadiusActive = true;
    blastRadiusNodes.clear();
    blastRadiusLinks.clear();
    
    nodeIDs.forEach(id => {
        blastRadiusNodes.add(id);
    });

    // Zoom and center graph slightly around the first smell node
    const firstNode = filteredNodes.find(n => n.id === nodeIDs[0]);
    if (firstNode) {
        d3.select(canvas).transition().duration(500).call(
            d3.zoom().transform,
            d3.zoomIdentity.translate(width / 2 - firstNode.x, height / 2 - firstNode.y)
        );
    }

    ticked();
}

// AI Narrative Chat
async function sendQuestion() {
    const input = document.getElementById("chat-input");
    const question = input.value.trim();
    if (question === "") return;

    const chatHistory = document.getElementById("chat-history");

    // Append User Message
    const userMsg = document.createElement("div");
    userMsg.className = "chat-message user";
    userMsg.textContent = question;
    chatHistory.appendChild(userMsg);
    input.value = "";
    chatHistory.scrollTop = chatHistory.scrollHeight;

    // Append Agent Loading Message
    const loadingMsg = document.createElement("div");
    loadingMsg.className = "chat-message agent";
    loadingMsg.textContent = "🤖 Formulating answer, querying serving layer...";
    chatHistory.appendChild(loadingMsg);
    chatHistory.scrollTop = chatHistory.scrollHeight;

    try {
        const resp = await fetch(`${config.serving}/v1/ask`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                question: question,
                namespace: config.namespace
            })
        });

        if (resp.ok) {
            const data = await resp.json();
            
            // Format answer text
            let answerHTML = "";
            if (data.answer) {
                // Parse simple markdown block stubs
                let text = data.answer.text;
                text = text.replace(/`([^`]+)`/g, "<code>$1</code>");
                text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
                text = text.replace(/\n/g, "<br>");
                
                answerHTML = `<p>${text}</p>`;
                if (data.answer.mermaid_diagram) {
                    answerHTML += `
                        <div style="margin-top:12px;">
                            <strong>Flowchart Output:</strong>
                            <pre><code>${data.answer.mermaid_diagram}</code></pre>
                        </div>
                    `;
                }
            } else if (data.blast_radius) {
                answerHTML = `<p><strong>Blast Radius Analysis:</strong> Affected ${data.blast_radius.total_affected} downstreams.</p>`;
            } else if (data.health_report) {
                answerHTML = `<p><strong>Health Audit Analysis:</strong> Found ${data.health_report.summary.cycles_found} dependency loops and ${data.health_report.smells.length} smells.</p>`;
            } else {
                answerHTML = `<p>Question compiled successfully. No narrative answer was generated.</p>`;
            }

            loadingMsg.innerHTML = `🤖 <strong>ArchGraph:</strong> ${answerHTML}`;
        } else {
            loadingMsg.textContent = "❌ Failed to query Serving serving engine.";
        }
    } catch (e) {
        loadingMsg.textContent = `❌ Error connecting to serving engine: ${e.message}`;
    }
    chatHistory.scrollTop = chatHistory.scrollHeight;
}

// Timeline Log Replayer
let isPlaying = false;
let timelineTimer = null;

function setupTimeline() {
    const slider = document.getElementById("timeline-slider");
    const ticks = document.getElementById("timeline-ticks");
    
    slider.max = logEntries.length;
    slider.value = logEntries.length;
    currentLogIndex = logEntries.length;
    
    // Draw simple tick points
    ticks.innerHTML = "";
    const stepCount = Math.min(logEntries.length, 10);
    for (let i = 0; i < stepCount; i++) {
        const tick = document.createElement("div");
        tick.className = "tick active";
        ticks.appendChild(tick);
    }

    slider.addEventListener("input", (e) => {
        currentLogIndex = parseInt(e.target.value);
        replayToCurrentState();
    });

    document.getElementById("btn-play-timeline").addEventListener("click", () => {
        if (isPlaying) {
            pauseTimeline();
        } else {
            playTimeline();
        }
    });
}

function playTimeline() {
    const slider = document.getElementById("timeline-slider");
    if (currentLogIndex >= logEntries.length) {
        currentLogIndex = 0;
        slider.value = 0;
    }
    
    isPlaying = true;
    document.getElementById("btn-play-timeline").textContent = "⏸ Pause";
    
    timelineTimer = setInterval(() => {
        if (currentLogIndex < logEntries.length) {
            currentLogIndex++;
            slider.value = currentLogIndex;
            replayToCurrentState();
        } else {
            pauseTimeline();
        }
    }, 800);
}

function pauseTimeline() {
    isPlaying = false;
    document.getElementById("btn-play-timeline").textContent = "▶ Play";
    if (timelineTimer) {
        clearInterval(timelineTimer);
        timelineTimer = null;
    }
}

// Reconstruct the graph state up to a specific transaction logs index
function replayToCurrentState() {
    if (currentLogIndex === -1 || logEntries.length === 0) return;

    document.getElementById("timeline-ticks").querySelectorAll(".tick").forEach((t, idx) => {
        const progress = currentLogIndex / logEntries.length;
        const tickProgress = idx / 10;
        t.className = tickProgress <= progress ? "tick active" : "tick";
    });

    // 0 index is the complete graph (since we can reconstruct up to that point)
    if (currentLogIndex === logEntries.length) {
        let commitLabel = "Live System";
        if (gitCommits && gitCommits.length > 0) {
            const latest = gitCommits[0];
            commitLabel = `Live System (${latest.short_sha}: ${latest.subject})`;
        }
        document.getElementById("timeline-commit-time").textContent = commitLabel;
        document.getElementById("timeline-mutation-desc").textContent = "Current state projection";
        refreshGraphData();
        return;
    }

    // Reconstruction
    let reconstructedNodes = [];
    let reconstructedLinks = [];
    
    const nodeMap = new Map();
    const linkMap = new Map();

    let currentSHA = null;
    let lastEntry = null;

    // Replay log entries up to currentLogIndex
    for (let i = 0; i < currentLogIndex; i++) {
        const entry = logEntries[i];
        lastEntry = entry;
        
        // Extract head_sha if available in node properties
        const props = entry.after_state?.properties || entry.before_state?.properties;
        if (props && props.head_sha) {
            currentSHA = props.head_sha;
        }

        // Apply mutations
        if (entry.mutation_type === "CREATE_ENTITY" || 
            entry.mutation_type === "UPSERT_ENTITY" || 
            entry.mutation_type === "ENTITY_CREATED" || 
            entry.mutation_type === "ENTITY_UPDATED" || 
            entry.mutation_type === "ENTITY_RESTORED") {
            const state = entry.after_state;
            if (state) {
                nodeMap.set(entry.entity_id, {
                    id: entry.entity_id,
                    type: state.type,
                    canonical_name: state.canonical_name,
                    sub_type: state.sub_type,
                    properties: state.properties,
                    confidence: state.confidence,
                    is_active: state.is_active,
                    namespace: state.namespace
                });
            }
        } else if (entry.mutation_type === "DELETE_ENTITY" || 
                   entry.mutation_type === "ENTITY_SOFT_DELETED") {
            nodeMap.delete(entry.entity_id);
        } else if (entry.mutation_type === "CREATE_RELATIONSHIP" || 
                   entry.mutation_type === "UPSERT_RELATIONSHIP" || 
                   entry.mutation_type === "RELATIONSHIP_CREATED" || 
                   entry.mutation_type === "RELATIONSHIP_UPDATED") {
            const state = entry.after_state;
            if (state) {
                linkMap.set(entry.relationship_id, {
                    id: entry.relationship_id,
                    type: state.type,
                    from_id: state.from_id,
                    to_id: state.to_id,
                    confidence: state.confidence,
                    is_active: state.is_active
                });
            }
        } else if (entry.mutation_type === "DELETE_RELATIONSHIP" || 
                   entry.mutation_type === "RELATIONSHIP_DELETED") {
            linkMap.delete(entry.relationship_id);
        }
    }

    // Update labels based on the final replayed state
    if (lastEntry) {
        const timeStr = new Date(lastEntry.occurred_at).toLocaleTimeString();
        let commitLabel = `${timeStr} (Log #${lastEntry.entry_id})`;
        if (currentSHA) {
            const commit = gitCommits.find(c => c.sha === currentSHA || c.short_sha === currentSHA);
            if (commit) {
                commitLabel = `${commit.date} (${commit.short_sha}: ${commit.subject})`;
            } else {
                commitLabel = `${timeStr} (Commit: ${currentSHA.substring(0, 7)})`;
            }
        }
        document.getElementById("timeline-commit-time").textContent = commitLabel;
        document.getElementById("timeline-mutation-desc").textContent = `${lastEntry.mutation_type} ${lastEntry.entity_id || lastEntry.relationship_id || ""}`;
    }

    // Convert back to arrays
    reconstructedNodes = Array.from(nodeMap.values()).filter(n => n.is_active);
    reconstructedLinks = Array.from(linkMap.values()).filter(l => l.is_active);

    // Apply filtering to the reconstructed graph
    const showServices = document.getElementById("filter-services").checked;
    const showModules = document.getElementById("filter-modules").checked;
    const showFunctions = document.getElementById("filter-functions").checked;
    const showEndpoints = document.getElementById("filter-endpoints").checked;
    const showDatabases = document.getElementById("filter-databases").checked;

    filteredNodes = reconstructedNodes.filter(node => {
        if (node.namespace && node.namespace !== config.namespace) return false;
        if (node.type === "SERVICE" && !showServices) return false;
        if (node.type === "MODULE" && !showModules) return false;
        if (node.type === "FUNCTION" && !showFunctions) return false;
        if (node.type === "API_ENDPOINT" && !showEndpoints) return false;
        if (node.type === "DATABASE_TABLE" && !showDatabases) return false;
        return true;
    });

    const activeIDs = new Set(filteredNodes.map(n => n.id));
    filteredLinks = reconstructedLinks.filter(l => activeIDs.has(l.from_id) && activeIDs.has(l.to_id)).map(l => ({
        id: l.id,
        type: l.type,
        source: l.from_id,
        target: l.to_id,
        confidence: l.confidence
    }));

    rebuildSimulation();
}

// Run Repository Ingestion API call
async function runRepositoryIngestion() {
    const urlInput = document.getElementById("ingest-repo-url");
    const nsInput = document.getElementById("ingest-namespace");
    const statusDiv = document.getElementById("ingest-status");

    const repoURL = urlInput.value.trim();
    let namespace = nsInput.value.trim();

    if (!repoURL) {
        statusDiv.className = "ingest-status error";
        statusDiv.textContent = "Please enter a repository URL.";
        statusDiv.classList.remove("hidden");
        return;
    }

    // Set loading state
    statusDiv.className = "ingest-status loading";
    statusDiv.textContent = "📥 Cloning, scanning, and ingesting repository... This can take up to a minute.";
    statusDiv.classList.remove("hidden");
    
    const btn = document.getElementById("btn-ingest-repo");
    btn.disabled = true;

    try {
        const resp = await fetch("/api/ingest", {
            method: "POST",
            headers: {
                "Content-Type": "application/json"
            },
            body: JSON.stringify({
                repo_url: repoURL,
                namespace: namespace,
                languages: ["go"] // Default to scanning Go codebases
            })
        });

        btn.disabled = false;

        if (resp.ok) {
            const data = await resp.json();
            
            // Clean inputs
            urlInput.value = "";
            nsInput.value = "";
            
            // If the backend auto-generated or completed under a specific namespace, use it
            if (data.namespace) {
                config.namespace = data.namespace;
            } else if (namespace) {
                config.namespace = namespace;
            } else {
                // Infer namespace from the repo url
                const cleanURL = repoURL.replace(/\.git$/, "");
                const parts = cleanURL.split("/");
                config.namespace = parts[parts.length - 1] || "local";
            }

            statusDiv.className = "ingest-status success";
            statusDiv.textContent = `✅ Ingestion successful! Loaded namespace: "${config.namespace}"`;
            
            // Update stats bar namespace label
            document.getElementById("stat-namespace").textContent = config.namespace;

            // Trigger reload of graph visualizer
            await refreshGraphData();
            await loadTransactionLogs();

            // Auto-hide status after 5 seconds
            setTimeout(() => {
                statusDiv.classList.add("hidden");
            }, 5000);
        } else {
            const errMsg = await resp.text();
            statusDiv.className = "ingest-status error";
            statusDiv.textContent = `❌ Ingestion failed: ${errMsg}`;
        }
    } catch (err) {
        btn.disabled = false;
        statusDiv.className = "ingest-status error";
        statusDiv.textContent = `❌ Connection error: ${err.message}`;
    }
}

// Wiki UI state
let wikiData = null;

// Unified view switcher
function switchView(view) {
    window.location.hash = view;
    
    // Toggle active view panes
    const viewHome = document.getElementById("view-home");
    const viewGraph = document.getElementById("view-graph");
    const viewWiki = document.getElementById("view-wiki");
    
    // Panes use .active for their display layout (grid/flex); .hidden forces them off.
    if (viewHome) { viewHome.classList.toggle("hidden", view !== "home"); viewHome.classList.toggle("active", view === "home"); }
    if (viewGraph) { viewGraph.classList.toggle("hidden", view !== "graph"); viewGraph.classList.toggle("active", view === "graph"); }
    if (viewWiki) { viewWiki.classList.toggle("hidden", view !== "wiki"); viewWiki.classList.toggle("active", view === "wiki"); }
    
    // Toggle active tabs in nav
    const tabGraph = document.getElementById("tab-graph");
    const tabWiki = document.getElementById("tab-wiki");
    if (tabGraph) tabGraph.classList.toggle("active", view === "graph");
    if (tabWiki) tabWiki.classList.toggle("active", view === "wiki");
    
    // Toggle header parts visibility
    const navTabs = document.querySelector(".view-tabs");
    const statsBar = document.querySelector(".stats-bar");
    const serverStatus = document.querySelector(".server-status");
    const footer = document.querySelector(".bottom-timeline");
    const container = document.querySelector(".dashboard-container");
    
    if (view === "home") {
        if (navTabs) navTabs.classList.add("hidden");
        if (statsBar) statsBar.classList.add("hidden");
        if (serverStatus) serverStatus.classList.add("hidden");
        if (footer) footer.classList.add("hidden");
        if (container) container.style.gridTemplateRows = "64px 1fr 0px";
    } else {
        if (navTabs) navTabs.classList.remove("hidden");
        if (statsBar) statsBar.classList.remove("hidden");
        if (serverStatus) serverStatus.classList.remove("hidden");
        if (footer) footer.classList.remove("hidden");
        if (container) container.style.gridTemplateRows = "64px 1fr 80px";
        
        if (view === "wiki") {
            loadWiki();
        }
    }
}

// Static fixture so the information display renders without a running backend (?demo=1).
const WIKI_DEMO = {
    namespace: "flutter",
    repo_url: "https://github.com/flutter/flutter",
    generated_at: "2026-06-02",
    topics: [
        { id: "overview", title: "Repository Overview", summary: "What this codebase contains and how its parts fit together." },
        { id: "framework", title: "The Flutter Framework", summary: "Foundational libraries for UI, animation, and rendering." },
        { id: "engine", title: "Engine Development", summary: "The C++ runtime that renders frames and talks to the platform." },
        { id: "testing", title: "Testing Libraries", summary: "Unit, widget, golden, and integration testing." },
        { id: "tooling", title: "The Command-Line Tool", summary: "Creating, building, and deploying applications." }
    ],
    pages: {
        overview: {
            title: "Repository Overview",
            summary: "This repository contains the Flutter framework, engine, and command-line tools, providing the foundational components for building cross-platform applications across mobile, web, and desktop.",
            used_llm: "gemini",
            citations: [1, 2, 3],
            markdown: [
                "The Flutter project facilitates code contributions through a defined set of guidelines covering environment setup, coding standards, and a structured pull request workflow. Its primary command-line tool orchestrates application creation, building, debugging, and deployment across target platforms.",
                "",
                "The project encompasses several key areas:",
                "",
                "- The framework provides foundational libraries for UI, animation, rendering, and gesture recognition. See [packages/flutter/lib/src/widgets/framework.dart](packages/flutter/lib/src/widgets/framework.dart#L120) for the core widget classes.",
                "- The underlying engine handles rendering and interaction with platform services. Entry points live in [engine/src/flutter/shell/common/shell.cc](engine/src/flutter/shell/common/shell.cc#L48).",
                "- The command-line tool drives the developer workflow from [packages/flutter_tools/bin/flutter.dart](packages/flutter_tools/bin/flutter.dart#L12).",
                "",
                "> Documentation is generated from the current state of the repository, so it stays aligned with the code on the default branch.",
                "",
                "## Architecture at a glance",
                "",
                "The layers build on one another: the framework renders into the engine, which presents to the host platform.",
                "",
                "```mermaid",
                "graph TD",
                "  A[Your App] --> B[Framework: widgets, rendering]",
                "  B --> C[Engine: Skia, Dart runtime]",
                "  C --> D[Embedder: iOS / Android / Web / Desktop]",
                "```",
                "",
                "## Contributing",
                "",
                "Before sending a pull request, run the analyzer and the test suite. The contribution guide describes the full workflow, including how to format code and write tests.",
                "",
                "```bash",
                "flutter analyze",
                "flutter test",
                "```"
            ].join("\n")
        }
    }
};

// Load and render dynamic wiki
async function loadWiki() {
    const nsInput = document.getElementById("wiki-ns-input");
    if (nsInput) nsInput.value = config.namespace;

    if (DEMO) {
        wikiData = WIKI_DEMO;
        if (nsInput) nsInput.value = wikiData.namespace;
        renderWikiTOC();
        selectWikiTopic(wikiData.topics[0].id);
        return;
    }

    const url = `${config.serving}/v1/wiki?namespace=${encodeURIComponent(config.namespace)}`;
    const content = document.getElementById("wiki-content");
    content.innerHTML = '<div class="placeholder spinner">Generating wiki…</div>';
    
    try {
        const res = await fetch(url);
        if (!res.ok) {
            const body = await res.text();
            showWikiError(`Wiki generation failed (${res.status}). ${body}`);
            return;
        }
        wikiData = await res.json();
        renderWikiTOC();
        if (wikiData.topics && wikiData.topics.length) {
            selectWikiTopic(wikiData.topics[0].id);
        } else {
            showWikiError("The wiki is empty — has this namespace been ingested yet?");
        }
    } catch (e) {
        showWikiError("Could not reach the serving layer at " + config.serving + ". Is the stack running?");
    }
}

function renderWikiTOC() {
    const tocList = document.getElementById("wiki-toc-list");
    tocList.innerHTML = "";
    // Compact "On this page" tree: title only, like Google CodeWiki.
    (wikiData.topics || []).forEach(t => {
        const div = document.createElement("div");
        div.className = "toc-item";
        div.id = "wiki-toc-" + t.id;
        div.title = t.summary || t.title;
        div.onclick = () => selectWikiTopic(t.id);
        div.textContent = t.title;
        tocList.appendChild(div);
    });
    updateWikiMeta();
}

// Populate the sidebar metadata footer (Updated on / Commit) from git history.
function updateWikiMeta() {
    const updatedEl = document.getElementById("wiki-updated");
    const commitEl = document.getElementById("wiki-commit");
    const latest = (typeof gitCommits !== "undefined" && gitCommits && gitCommits.length)
        ? gitCommits[0] : null;

    if (updatedEl) {
        let when = (wikiData && wikiData.generated_at) || (latest && (latest.date || latest.time));
        if (when) {
            const d = new Date(when);
            updatedEl.textContent = isNaN(d)
                ? String(when)
                : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
        } else {
            updatedEl.textContent = new Date().toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
        }
    }

    if (commitEl) {
        const hash = latest && (latest.hash || latest.sha || latest.id);
        if (hash) {
            const short = String(hash).slice(0, 7);
            const repo = (wikiData && wikiData.repo_url) || "";
            commitEl.innerHTML = repo
                ? `<a class="wiki-commit-link" href="${escapeHtml(repo)}/tree/${escapeHtml(hash)}" target="_blank" rel="noopener">${escapeHtml(short)}</a>`
                : `<code>${escapeHtml(short)}</code>`;
        } else {
            commitEl.textContent = "—";
        }
    }
}

function selectWikiTopic(id) {
    document.querySelectorAll(".wiki-toc-list .toc-item").forEach(el => el.classList.remove("active"));
    const tocEl = document.getElementById("wiki-toc-" + id);
    if (tocEl) tocEl.classList.add("active");
    
    const page = wikiData.pages && wikiData.pages[id];
    const content = document.getElementById("wiki-content");
    if (!page) {
        content.innerHTML = `<div class="placeholder">No page generated for this topic.</div>`;
        return;
    }
    
    const repo = (wikiData && wikiData.repo_url) || "";
    const ghLink = repo
        ? `<a class="gh-link" href="${escapeHtml(repo)}" target="_blank" rel="noopener" title="View repository on GitHub" aria-label="View repository on GitHub">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.3.8-.6v-2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.7-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z"/></svg>
            </a>`
        : "";

    const geminiPill = `<span class="gemini-pill">
            <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true">
                <defs><linearGradient id="pill-spark" x1="2" y1="2" x2="22" y2="22" gradientUnits="userSpaceOnUse">
                    <stop offset="0%" stop-color="#4285F4"/><stop offset="50%" stop-color="#9B72CB"/><stop offset="100%" stop-color="#D96570"/>
                </linearGradient></defs>
                <path d="M12 2c.4 4.6 2.4 6.6 7 7-4.6.4-6.6 2.4-7 7-.4-4.6-2.4-6.6-7-7 4.6-.4 6.6-2.4 7-7z" fill="url(#pill-spark)"/>
            </svg>Powered by Gemini</span>`;

    // The summary may arrive as markdown; render it as clean prose (CodeWiki shows a plain lead).
    const summaryText = mdToText(page.summary);

    content.innerHTML =
        `<div class="wiki-article-head"><h1>${escapeHtml(page.title)}</h1>${geminiPill}${ghLink}</div>` +
        (summaryText ? `<p class="page-summary">${escapeHtml(summaryText)}</p>` : "") +
        `<div id="wiki-page-body"></div>`;
        
    const body = document.getElementById("wiki-page-body");
    body.innerHTML = window.marked ? marked.parse(page.markdown || "") : escapeHtml(page.markdown || "");
    
    renderWikiMermaid(body);
    wireWikiCodeLinks(body);
    content.scrollTop = 0;
}

function renderWikiMermaid(root) {
    if (!window.mermaid) return;
    root.querySelectorAll("code.language-mermaid").forEach((code, i) => {
        const div = document.createElement("div");
        div.className = "mermaid";
        div.textContent = code.textContent;
        const pre = code.closest("pre");
        (pre || code).replaceWith(div);
    });
    try {
        const isLight = document.documentElement.classList.contains("light-theme");
        mermaid.initialize({
            startOnLoad: false,
            theme: isLight ? "neutral" : "dark",
            themeVariables: { fontFamily: "Inter, sans-serif" }
        });
        mermaid.run({ nodes: root.querySelectorAll(".mermaid") });
    } catch (e) {
        // diagram syntax issues shouldn't break the page
    }
}

function wireWikiCodeLinks(root) {
    root.querySelectorAll("a").forEach(a => {
        const href = a.getAttribute("href") || "";
        if (/^https?:\/\//.test(href) || href.startsWith("#") || href.startsWith("mailto:")) return;
        const m = href.match(/^([^#?]+?)(?:#L(\d+))?$/);
        if (!m) return;
        const path = m[1];
        const line = m[2] ? parseInt(m[2], 10) : 0;
        if (!/\.[a-zA-Z0-9]+$/.test(path)) return; // looks like a file
        a.classList.add("codelink");
        a.href = "javascript:void(0)";
        a.onclick = (ev) => {
            ev.preventDefault();
            openSrc(path, line);
        };
    });
}

// Wiki Q&A Assistant Chat
async function askWikiAssistant(qText) {
    const chatInput = document.getElementById("wiki-chat-input");
    if (qText) {
        chatInput.value = qText;
    }
    const question = chatInput.value.trim();
    if (!question) return;
    
    const chatHistory = document.getElementById("wiki-chat-history");

    // Clear the "Hi there!" empty state once a conversation begins.
    const emptyState = document.getElementById("wiki-assistant-empty");
    if (emptyState) emptyState.remove();

    // User message
    const userMsg = document.createElement("div");
    userMsg.className = "chat-message user";
    userMsg.textContent = question;
    chatHistory.appendChild(userMsg);
    chatInput.value = "";
    chatHistory.scrollTop = chatHistory.scrollHeight;
    
    // Agent loader message
    const agentMsg = document.createElement("div");
    agentMsg.className = "chat-message agent";
    agentMsg.textContent = "🤖 Reasoning over codebase...";
    chatHistory.appendChild(agentMsg);
    chatHistory.scrollTop = chatHistory.scrollHeight;
    
    try {
        const resp = await fetch(`${config.serving}/v1/ask`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                question: question,
                namespace: config.namespace
            })
        });
        
        if (resp.ok) {
            const data = await resp.json();
            let answerHTML = "";
            if (data.answer) {
                let text = data.answer.text;
                text = text.replace(/`([^`]+)`/g, "<code>$1</code>");
                text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
                text = text.replace(/\n/g, "<br>");
                answerHTML = `<p>${text}</p>`;
                if (data.answer.mermaid_diagram) {
                    answerHTML += `
                        <div style="margin-top:12px;">
                            <strong>Visual Flow:</strong>
                            <pre><code>${data.answer.mermaid_diagram}</code></pre>
                        </div>
                    `;
                }
            } else if (data.blast_radius) {
                answerHTML = `<p><strong>Blast Radius Analysis:</strong> Affected ${data.blast_radius.total_affected} downstreams.</p>`;
            } else if (data.health_report) {
                answerHTML = `<p><strong>Health Audit Analysis:</strong> Found ${data.health_report.summary.cycles_found} loops and ${data.health_report.smells.length} smells.</p>`;
            } else {
                answerHTML = `<p>Question processed. No narrative answer returned.</p>`;
            }
            agentMsg.innerHTML = `🤖 <strong>ArchGraph:</strong> ${answerHTML}`;
        } else {
            agentMsg.textContent = "❌ Failed to query serving layer.";
        }
    } catch (e) {
        agentMsg.textContent = `❌ Connection error: ${e.message}`;
    }
    chatHistory.scrollTop = chatHistory.scrollHeight;
}

// Unified source viewer modal handlers
async function openSrc(path, line) {
    const title = document.getElementById("src-title");
    const bodyEl = document.getElementById("src-body");
    title.textContent = path + (line ? ":" + line : "");
    bodyEl.innerHTML = '<div class="spinner">loading…</div>';
    document.getElementById("src-modal").classList.add("open");
    
    const url = `/api/source?path=${encodeURIComponent(path)}&namespace=${encodeURIComponent(config.namespace)}`;
    try {
        const res = await fetch(url);
        const text = await res.text();
        if (!res.ok) {
            bodyEl.innerHTML = `<div class="placeholder">${escapeHtml(text || "file not found")}</div>`;
            return;
        }
        renderSource(bodyEl, text, line);
    } catch (e) {
        bodyEl.innerHTML = `<div class="placeholder">Could not load source.</div>`;
    }
}

function renderSource(el, text, line) {
    const lines = text.split("\n");
    const pad = String(lines.length).length;
    const html = lines
        .map((ln, i) => {
            const n = i + 1;
            const cls = n === line ? "hl" : "";
            const num = String(n).padStart(pad, " ");
            return `<span class="${cls}">${num}  ${escapeHtml(ln)}</span>`;
        })
        .join("\n");
    el.innerHTML = `<pre>${html}</pre>`;
    if (line) {
        const hl = el.querySelector(".hl");
        if (hl) hl.scrollIntoView({ block: "center" });
    }
}

function closeSrc() {
    document.getElementById("src-modal").classList.remove("open");
}

function showWikiError(msg) {
    document.getElementById("wiki-content").innerHTML = `<div class="placeholder">${escapeHtml(msg)}</div>`;
}

function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) =>
        ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
    );
}

// Render any markdown the backend may put in a short field down to clean prose.
function mdToText(md) {
    if (!md) return "";
    let html;
    try {
        html = window.marked ? marked.parse(String(md)) : String(md);
    } catch (e) {
        html = String(md);
    }
    const tmp = document.createElement("div");
    tmp.innerHTML = html;
    return tmp.textContent.replace(/\s+/g, " ").trim();
}

// HOME PAGE SEARCH & INGESTION LOGIC
async function handleSearchOrIngest() {
    const input = document.getElementById("search-repo-input");
    const statusDiv = document.getElementById("home-ingest-status");
    const query = input.value.trim();
    
    if (!query) {
        showHomeStatus("Please enter a repository URL or namespace.", "error");
        return;
    }
    
    const isGitURL = query.startsWith("http://") || 
                     query.startsWith("https://") || 
                     query.startsWith("git@") || 
                     query.includes("github.com") || 
                     query.includes(".git");
                     
    if (isGitURL) {
        showHomeStatus("📥 Cloning, scanning, and ingesting repository... This can take up to a minute.", "loading");
        const btn = document.getElementById("btn-search-repo");
        btn.disabled = true;
        input.disabled = true;
        
        try {
            const resp = await fetch("/api/ingest", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json"
                },
                body: JSON.stringify({
                    repo_url: query,
                    languages: ["go"]
                })
            });
            
            btn.disabled = false;
            input.disabled = false;
            
            if (resp.ok) {
                const data = await resp.json();
                if (data.namespace) {
                    config.namespace = data.namespace;
                } else {
                    const cleanURL = query.replace(/\.git$/, "");
                    const parts = cleanURL.split("/");
                    config.namespace = parts[parts.length - 1] || "local";
                }
                
                showHomeStatus(`✅ Ingestion successful! Loaded namespace: "${config.namespace}"`, "success");
                document.getElementById("stat-namespace").textContent = config.namespace;
                
                await refreshGraphData();
                await loadTransactionLogs();
                
                setTimeout(() => {
                    switchView("graph");
                    statusDiv.classList.add("hidden");
                }, 1000);
            } else {
                const errMsg = await resp.text();
                showHomeStatus(`❌ Ingestion failed: ${errMsg}`, "error");
            }
        } catch (err) {
            btn.disabled = false;
            input.disabled = false;
            showHomeStatus(`❌ Connection error: ${err.message}`, "error");
        }
    } else {
        showHomeStatus("🔍 Verifying namespace...", "loading");
        try {
            const resp = await fetch(`${config.serving}/v1/entities?namespace=${encodeURIComponent(query)}`);
            if (resp.ok) {
                config.namespace = query;
                document.getElementById("stat-namespace").textContent = config.namespace;
                
                await refreshGraphData();
                await loadTransactionLogs();
                
                showHomeStatus("✅ Loaded successfully!", "success");
                setTimeout(() => {
                    switchView("graph");
                    statusDiv.classList.add("hidden");
                }, 500);
            } else {
                showHomeStatus(`❌ Namespace "${query}" not found. Try entering a full Git repository URL to ingest it first.`, "error");
            }
        } catch (err) {
            showHomeStatus(`❌ Error connecting: ${err.message}`, "error");
        }
    }
}

function showHomeStatus(msg, type) {
    const statusDiv = document.getElementById("home-ingest-status");
    if (!statusDiv) return;
    statusDiv.className = `ingest-status ${type}`;
    statusDiv.textContent = msg;
    statusDiv.classList.remove("hidden");
}

function renderSuggestions() {
    const container = document.getElementById("suggestion-chips");
    if (!container) return;
    container.innerHTML = "";
    
    const defaultNs = config.namespace || "acme";
    const namespaces = new Set([defaultNs, "acme", "local", "local-dev"]);
    
    namespaces.forEach(ns => {
        const btn = document.createElement("button");
        btn.className = "suggest-chip";
        btn.textContent = ns;
        btn.onclick = () => {
            document.getElementById("search-repo-input").value = ns;
            handleSearchOrIngest();
        };
        container.appendChild(btn);
    });
}

function applyThemeIcon(btn, isLight) {
    if (!btn) return;
    btn.innerHTML = isLight ? `
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="5"></circle>
                    <line x1="12" y1="1" x2="12" y2="3"></line>
                    <line x1="12" y1="21" x2="12" y2="23"></line>
                    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line>
                    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line>
                    <line x1="1" y1="12" x2="3" y2="12"></line>
                    <line x1="21" y1="12" x2="23" y2="12"></line>
                    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line>
                    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line>
                </svg>
            ` : `
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path>
                </svg>
            `;
}

function setupThemeToggle() {
    const root = document.documentElement;
    const btn = document.getElementById("btn-theme-toggle");

    // The no-flash <head> script already set the initial class from
    // localStorage or the OS preference. Sync the icon to that state.
    applyThemeIcon(btn, root.classList.contains("light-theme"));

    if (btn) {
        btn.addEventListener("click", () => {
            const isLight = root.classList.toggle("light-theme");
            try { localStorage.setItem("theme", isLight ? "light" : "dark"); } catch (e) {}
            applyThemeIcon(btn, isLight);
        });
    }

    // Follow OS changes only while the user hasn't picked an explicit theme.
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    mq.addEventListener("change", (e) => {
        let stored = null;
        try { stored = localStorage.getItem("theme"); } catch (err) {}
        if (stored) return;
        root.classList.toggle("light-theme", e.matches);
        applyThemeIcon(btn, e.matches);
    });

    const btnHelp = document.getElementById("btn-help");
    if (btnHelp) {
        btnHelp.addEventListener("click", () => {
            alert("Code Wiki Landing Dashboard\\n\\nEnter a repository Git URL to scan and ingest, or type/click an existing namespace to explore its codebase blueprint and generated documentation.");
        });
    }
}

// Bootstrap
init();
