package fixture

// MaxAnimals is the herd cap.
const MaxAnimals = 100

var defaultName = "rex"

// Speaker is anything that speaks.
type Speaker interface {
	Speak() string
}

// Animal is a base struct.
type Animal struct {
	Name string
}

// Dog embeds Animal.
type Dog struct {
	Animal
	Tricks []string
}

// Speak returns the animal's sound.
func (a Animal) Speak() string { return "..." }

// NewAnimal builds an Animal.
func NewAnimal(name string) *Animal { return &Animal{Name: name} }
