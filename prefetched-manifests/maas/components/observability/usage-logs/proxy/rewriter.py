"""LogQL query rewriting to inject user filters."""


def escape_logql_value(value):
    """Escape special characters in LogQL values."""
    value = value.replace("\\", "\\\\")
    value = value.replace('"', '\\"')
    return value


def inject_user_filter(query, username, namespace, labels_only=False):
    """
    Inject user filter into LogQL query.

    Modifies stream selectors to add kubernetes_namespace_name label with the provided namespace.
    If labels_only=False, also adds pipeline filter `| user_id="<user>"` after the selector.

    Args:
        query: LogQL query string
        username: User identifier to filter by
        namespace: Kubernetes namespace to filter by
        labels_only: If True, skip pipeline filter (for label values endpoints)

    Examples:
        Normal query (labels_only=False):
            {app="x"} | line_format "{{.msg}}"
         -> {app="x", kubernetes_namespace_name="<namespace>"} | user_id="alice" | line_format "{{.msg}}"

        Empty selector (labels_only=False):
            {}
         -> {kubernetes_namespace_name="<namespace>"} | user_id="alice"

        Label values query (labels_only=True):
            {app="x"}
         -> {app="x", kubernetes_namespace_name="<namespace>"}
    """
    if not query or not username:
        return query

    namespace_label = f'kubernetes_namespace_name="{escape_logql_value(namespace)}"'
    user_filter = '' if labels_only else f' | user_id="{escape_logql_value(username)}"'
    result = []
    in_selector = False
    selector_start_pos = -1
    in_double_quote = False
    in_backtick = False
    i = 0

    while i < len(query):
        ch = query[i]

        # Handle escape sequences in double quotes
        if ch == "\\" and in_double_quote and i + 1 < len(query):
            result.append(ch)
            i += 1
            result.append(query[i])
            i += 1
            continue

        # Toggle quote states
        if ch == '"' and not in_backtick:
            in_double_quote = not in_double_quote
        if ch == "`" and not in_double_quote:
            in_backtick = not in_backtick

        in_quote = in_double_quote or in_backtick

        # Track selector braces
        if ch == "{" and not in_quote:
            in_selector = True
            selector_start_pos = len(result)
            result.append(ch)
            i += 1
            continue

        # Inject namespace label and optionally user filter before closing selector brace
        if ch == "}" and in_selector and not in_quote:
            # Check if selector is empty by looking at content since '{'
            selector_content = "".join(result[selector_start_pos + 1:]).strip()
            is_empty = len(selector_content) == 0

            # Add comma only if selector has existing matchers
            if is_empty:
                result.append(namespace_label)
            else:
                result.append(", ")
                result.append(namespace_label)

            result.append(ch)
            result.append(user_filter)
            in_selector = False
        else:
            result.append(ch)

        i += 1

    return "".join(result)
