// Minimal GraphQL client + renderer. No framework, no build step.
//
// To complete Part 3, you will need to:
//   1. Add the new field you chose to the `QUERY` below.
//   2. Render it inside renderUser / renderTask.


const ENDPOINT = "http://localhost:8080/graphql";

// $status is optional: when null/omitted, the API applies no filter
// (flows down to store.TaskFilter{Status: nil} → no WHERE clause).
const QUERY = `
  query Board($status: TaskStatus) {
    users {
      id
      name
      email
      tasks(status: $status) {
        id
        title
        status
        dueDate
        tags
      }
    }
  }
`;

async function gqlFetch(query, variables = {}) {
  const res = await fetch(ENDPOINT, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ query, variables }),
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  }
  const body = await res.json();
  if (body.errors && body.errors.length > 0) {
    throw new Error(body.errors.map((e) => e.message).join("; "));
  }
  return body.data;
}

function el(tag, attrs = {}, children = []) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else node.setAttribute(k, v);
  }
  for (const child of [].concat(children)) {
    if (child == null) continue;
    node.appendChild(typeof child === "string" ? document.createTextNode(child) : child);
  }
  return node;
}

function renderTask(task) {
  const title = el("div", { class: "task-title", text: task.title });

  const statusPill = el("span", {
    class: `status-pill ${task.status.toLowerCase()}`,
    text: task.status.replace("_", " "),
  });

  const meta = el("div", { class: "task-meta" }, [statusPill]);

  // Part 3: dueDate is nullable — render a chip only when a date exists,
  // so tasks without one get nothing (not "due null"). Highlight overdue
  // tasks (past due and not done); ISO dates compare correctly as strings.
  if (task.dueDate) {
    const today = new Date().toISOString().slice(0, 10);
    const overdue = task.status !== "DONE" && task.dueDate < today;
    meta.appendChild(
      el("span", {
        class: overdue ? "due-date overdue" : "due-date",
        text: `due ${task.dueDate}`,
      })
    );
  }

  for (const tag of task.tags || []) {
    meta.appendChild(el("span", { class: "tag", text: tag }));
  }

  return el("li", { class: "task" }, [title, meta]);
}

function renderUser(user) {
  const heading = el("h2", { text: user.name });
  const email = el("p", { class: "email", text: user.email });

  const tasks = el(
    "ul",
    { class: "task-list" },
    (user.tasks || []).map(renderTask)
  );

  return el("section", { class: "user-card" }, [heading, email, tasks]);
}

function setStatus(text, isError = false) {
  const el = document.getElementById("status");
  el.textContent = text;
  el.classList.toggle("error", !!isError);
}

async function main() {
  document.getElementById("endpoint").textContent = ENDPOINT;
  setStatus("Loading.");

  const filter = document.getElementById("statusFilter").value;
  const variables = filter ? { status: filter } : {};

  try {
    const data = await gqlFetch(QUERY, variables);
    const root = document.getElementById("app");
    root.innerHTML = "";
    if (!data.users || data.users.length === 0) {
      root.appendChild(el("p", { class: "empty", text: "No users to show." }));
    } else {
      for (const user of data.users) root.appendChild(renderUser(user));
    }
    setStatus(`Loaded ${data.users.length} user(s).`);
  } catch (err) {
    setStatus(`Failed to load: ${err.message}`, true);
    console.error(err);
  }
}

document.getElementById("statusFilter").addEventListener("change", main);

main();