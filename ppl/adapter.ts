import { BaseErrorListener, CharStream, CommonTokenStream, ParserRuleContext, TerminalNode } from "antlr4ng";
import { OpenSearchPPLLexer } from "../.omx/ppl-upstream/packages/osd-antlr-grammar/src/opensearch_ppl/.generated/OpenSearchPPLLexer";
import { OpenSearchPPLParser } from "../.omx/ppl-upstream/packages/osd-antlr-grammar/src/opensearch_ppl/.generated/OpenSearchPPLParser";

class RejectedInput extends Error {}

class RejectError extends BaseErrorListener {
  override syntaxError(): void {
    throw new RejectedInput("PPL syntax error");
  }
}

export function parse(input: string): string {
  try {
    if (input.length === 0 || input.length > 32768) throw new RejectedInput("input limit");
    const lexer = new OpenSearchPPLLexer(CharStream.fromString(input));
    lexer.removeErrorListeners();
    lexer.addErrorListener(new RejectError());
    const tokens = new CommonTokenStream(lexer);
    tokens.fill();
    if (tokens.getTokens().some((token) => token.type === OpenSearchPPLLexer.ERROR_RECOGNITION || (token.channel !== 0 && token.channel !== 1))) throw new RejectedInput("invalid token");
    const parser = new OpenSearchPPLParser(tokens);
    parser.removeErrorListeners();
    parser.addErrorListener(new RejectError());
    const root = parser.root();
    let count = 0;
    const walk = (node: unknown, depth: number): unknown => {
      if (++count > 5000 || depth > 256) throw new RejectedInput("tree limit");
      if (node instanceof TerminalNode) return { token: node.symbol.type, text: node.getText() };
      if (!(node instanceof ParserRuleContext)) throw new Error("invalid PPL tree node");
      const children = [];
      for (let i = 0; i < node.getChildCount(); i++) children.push(walk(node.getChild(i), depth + 1));
      return { rule: parser.ruleNames[node.ruleIndex], children };
    };
    return JSON.stringify({ root: walk(root, 0) });
  } catch (error) {
    if (error instanceof RejectedInput) return JSON.stringify({ error: "PPL syntax or resource limit" });
    throw error;
  }
}
