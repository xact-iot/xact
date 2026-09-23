import jsep from 'jsep';
import { escapeHtml, sanitizeHtml } from './html-sanitize';

type Value = string | number | boolean | null | undefined;
interface Context {
  deviceName: string;
  deviceDescription: string;
  tag: (path: string) => unknown;
}

function primitive(value: unknown): Value {
  if (value === null || value === undefined || ['string', 'number', 'boolean'].includes(typeof value)) return value as Value;
  return '';
}

// Parse expressions as data. No properties, assignment, globals, or general calls.
function evaluate(node: jsep.Expression, context: Context, depth = 0): Value {
  if (depth > 32) throw new Error('Template expression is too deeply nested');
  const read = (child: jsep.Expression) => evaluate(child, context, depth + 1);
  switch (node.type) {
    case 'Literal': return primitive((node as jsep.Literal).value);
    case 'Identifier': {
      const name = (node as jsep.Identifier).name;
      if (name === 'deviceName') return context.deviceName;
      if (name === 'deviceDescription') return context.deviceDescription;
      throw new Error('Unknown template variable');
    }
    case 'CallExpression': {
      const call = node as jsep.CallExpression;
      const arg = call.arguments[0] as jsep.Literal | undefined;
      if (call.callee.type !== 'Identifier' || (call.callee as jsep.Identifier).name !== 'tag' ||
          call.arguments.length !== 1 || arg?.type !== 'Literal' || typeof arg.value !== 'string') {
        throw new Error('Only tag("path") calls are supported');
      }
      return primitive(context.tag(arg.value));
    }
    case 'UnaryExpression': {
      const unary = node as jsep.UnaryExpression;
      const value = read(unary.argument);
      if (unary.operator === '!') return !value;
      if (unary.operator === '-') return -Number(value);
      if (unary.operator === '+') return Number(value);
      throw new Error('Unsupported unary operator');
    }
    case 'BinaryExpression': {
      const binary = node as jsep.BinaryExpression;
      const left = read(binary.left);
      if (binary.operator === '&&') return left && read(binary.right);
      if (binary.operator === '||') return left || read(binary.right);
      if (binary.operator === '??') return left ?? read(binary.right);
      const right = read(binary.right);
      switch (binary.operator) {
        case '==': return left == right;
        case '!=': return left != right;
        case '===': return left === right;
        case '!==': return left !== right;
        case '>': return Number(left) > Number(right);
        case '>=': return Number(left) >= Number(right);
        case '<': return Number(left) < Number(right);
        case '<=': return Number(left) <= Number(right);
        case '+': return typeof left === 'string' || typeof right === 'string' ? String(left) + String(right) : Number(left) + Number(right);
        case '-': return Number(left) - Number(right);
        case '*': return Number(left) * Number(right);
        case '/': return Number(left) / Number(right);
        case '%': return Number(left) % Number(right);
        default: throw new Error('Unsupported binary operator');
      }
    }
    case 'ConditionalExpression': {
      const conditional = node as jsep.ConditionalExpression;
      return read(conditional.test) ? read(conditional.consequent) : read(conditional.alternate);
    }
    default: throw new Error('Unsupported template expression');
  }
}

export function renderMapTemplate(template: string, context: Context): string {
  if (template.length > 100_000) throw new Error('Template is too large');
  const html = template.replace(/\$\{([^{}]*)\}/g, (_match, expression: string) => {
    if (expression.length > 2000) throw new Error('Template expression is too large');
    const normalized = expression.replace(/\btag\(([a-zA-Z_$][a-zA-Z0-9_$]*(?:\.[a-zA-Z_$][a-zA-Z0-9_$]*)+(?::[a-zA-Z_][a-zA-Z0-9_-]*)?)\)/g, "tag('$1')");
    return escapeHtml(evaluate(jsep(normalized), context));
  });
  if (/\$\{/.test(html)) throw new Error('Invalid template expression');
  return sanitizeHtml(html);
}
