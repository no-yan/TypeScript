import { A } from "./a";

export class B {
    #x = "b";
    #m() { return this.#x; }
    getX() { return this.#m(); }
    static isB(o: object): o is B { return #x in o; }
}

const a = new A().getX();
const b = new B().getX();
function f(o: A | B) {
    if (A.isA(o)) {
        return o.getX();
    }
    return o.getX();
}
