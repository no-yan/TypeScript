class A {
    #x = 1;
    getA() { return this.#x; }
    static has(o: object) { return #x in o; }
}
class B {
    #x = "b";
    getB() { return this.#x; }
    static has(o: object) { return #x in o; }
}
const a = new A().getA();
const b = new B().getB();
