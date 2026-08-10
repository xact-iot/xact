import { cloneValue } from '../utils/clone';
import type { GraphDocument } from './types';

export class DraftStore {
  private graph: GraphDocument;
  private saved: string;
  private undoStack: GraphDocument[] = [];
  private redoStack: GraphDocument[] = [];

  constructor(graph: GraphDocument) { this.graph = cloneValue(graph); this.saved = JSON.stringify(this.graph); }
  get value(): GraphDocument { return cloneValue(this.graph); }
  get dirty(): boolean { return JSON.stringify(this.graph) !== this.saved; }
  get canUndo(): boolean { return this.undoStack.length > 0; }
  get canRedo(): boolean { return this.redoStack.length > 0; }
  update(next: GraphDocument): void { this.undoStack.push(this.graph); if (this.undoStack.length > 100) this.undoStack.shift(); this.graph = cloneValue(next); this.redoStack = []; }
  undo(): GraphDocument { const value = this.undoStack.pop(); if (value) { this.redoStack.push(this.graph); this.graph = value; } return this.value; }
  redo(): GraphDocument { const value = this.redoStack.pop(); if (value) { this.undoStack.push(this.graph); this.graph = value; } return this.value; }
  markSaved(graph: GraphDocument): void { this.graph = cloneValue(graph); this.saved = JSON.stringify(this.graph); this.undoStack = []; this.redoStack = []; }
}
