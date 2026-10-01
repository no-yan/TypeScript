// The first class of its file, as B is of b.ts: the two class symbols have
// the same local SymbolId in their Bounds.
export class A {
    #x = 1;
    #m() { return this.#x; }
    getX() { return this.#m(); }
    static isA(o: object): o is A { return #x in o; }
}
