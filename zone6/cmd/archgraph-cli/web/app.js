// ArchGraph Web Dashboard Controller
let config = {
    zone4: "http://localhost:8080",
    zone5: "http://localhost:8081",
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

// Selected & Highlight States
let selectedNode = null;
let hoveredNode = null;
let blastRadiusNodes = new Set();
let blastRadiusLinks = new Set();
let isBlastRadiusActive = false;

// UI Panels
let activeTab = 'inspector'; // inspector | ask

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
    
    // Fetch data
    await refreshGraphData();
    await loadTransactionLogs();
    
    // Initialize UI event handlers
    setupEventHandlers();
    
    // Setup D3 zoom
    d3.select(canvas).call(
        d3.zoom()
            .scaleExtent([0.1, 8])
            .on("zoom", (event) => {
                transform = event.transform;
                ticked();
            })
    );
}

// Fetch main graph data and health smells
async function refreshGraphData() {
    try {
        const u = `${config.zone5}/v1/entities?namespace=${encodeURIComponent(config.namespace)}`;
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
        const resp = await fetch(`${config.zone5}/v1/health-audit`, {
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
        const resp = await fetch(`${config.zone5}/v1/log?limit=200`);
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
        const resp = await fetch(`${config.zone5}/v1/blast-radius`, {
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
        const resp = await fetch(`${config.zone5}/v1/ask`, {
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
            loadingMsg.textContent = "❌ Failed to query Zone 5 serving engine.";
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
        document.getElementById("timeline-commit-time").textContent = "Live System";
        document.getElementById("timeline-mutation-desc").textContent = "Current state projection";
        refreshGraphData();
        return;
    }

    // Reconstruction
    let reconstructedNodes = [];
    let reconstructedLinks = [];
    
    const nodeMap = new Map();
    const linkMap = new Map();

    // Replay log entries up to currentLogIndex
    for (let i = 0; i < currentLogIndex; i++) {
        const entry = logEntries[i];
        const timeStr = new Date(entry.occurred_at).toLocaleTimeString();
        document.getElementById("timeline-commit-time").textContent = `${timeStr} (Log #${entry.entry_id})`;
        document.getElementById("timeline-mutation-desc").textContent = `${entry.mutation_type} ${entry.entity_id || entry.relationship_id || ""}`;

        // Apply mutations
        if (entry.mutation_type === "CREATE_ENTITY" || entry.mutation_type === "UPSERT_ENTITY") {
            const state = entry.after_state;
            if (state) {
                nodeMap.set(entry.entity_id, {
                    id: entry.entity_id,
                    type: state.type,
                    canonical_name: state.canonical_name,
                    sub_type: state.sub_type,
                    properties: state.properties,
                    confidence: state.confidence,
                    is_active: state.is_active
                });
            }
        } else if (entry.mutation_type === "DELETE_ENTITY") {
            nodeMap.delete(entry.entity_id);
        } else if (entry.mutation_type === "CREATE_RELATIONSHIP" || entry.mutation_type === "UPSERT_RELATIONSHIP") {
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
        } else if (entry.mutation_type === "DELETE_RELATIONSHIP") {
            linkMap.delete(entry.relationship_id);
        }
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

// Bootstrap
init();
