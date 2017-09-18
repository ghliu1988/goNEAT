package genetics

import (
	"github.com/yaricom/goNEAT/neat"
	"sort"
	"math"
	"fmt"
	"errors"
	"math/rand"
	"github.com/yaricom/goNEAT/neat/network"
)

// A Species is a group of similar Organisms.
// Reproduction takes place mostly within a single species, so that compatible organisms can mate.
type Species struct {
	// The ID
	Id                   int;
	// The age of the Species
	Age                  int
	// The average fitness of the Species
	AvgFitness           float64
	// The maximal fitness of the Species
	MaxFitness           float64
	// The maximal fitness it ever had
	MaxFitnessEver       float64
	// How many child expected
	ExpectedOffspring    int

	// Is it novel
	IsNovel              bool
	// Is it tested
	IsChecked            bool

	// The organisms in the Species
	Organisms            []*Organism
	// If this is too long ago, the Species will goes extinct
	AgeOfLastImprovement int
}

// Construct new species with specified ID
func NewSpecies(id int) *Species {
	return newSpecies(id)
}

// Allows the creation of a Species that won't age (a novel one). This protects new Species from aging
// inside their first generation
func NewSpeciesNovel(id int, novel bool) *Species {
	s := newSpecies(id)
	s.IsNovel = novel

	return s
}

// The private default constructor
func newSpecies(id int) *Species {
	return &Species{
		Id:id,
		Age:1,
		Organisms:make([]*Organism, 0),
	}
}


// Adds new Organism to the group related to this Species
func (s *Species) addOrganism(o *Organism) {
	s.Organisms = append(s.Organisms, o)
}
// Removes an organism from Species
func (s *Species) removeOrganism(org *Organism) (bool, error) {
	orgs := make([]*Organism, 0)
	for _, o := range s.Organisms {
		if o != org {
			orgs = append(orgs, o)
		}
	}
	if len(orgs) != len(s.Organisms) - 1 {
		return false, errors.New("ALERT: Attempt to remove nonexistent Organism from Species")
	} else {
		s.Organisms = orgs
		return true, nil
	}
}

// Can change the fitness of the organisms in the Species to be higher for very new species (to protect them).
// Divides the fitness by the size of the Species, so that fitness is "shared" by the species.
func (s *Species) adjustFitness(conf *neat.Neat) {
	age_debt := (s.Age - s.AgeOfLastImprovement + 1) - conf.DropOffAge
	if age_debt == 0 {
		age_debt = 1
	}

	for _, org := range s.Organisms {
		// Remember the original fitness before it gets modified
		org.OriginalFitness = org.Fitness

		// Make fitness decrease after a stagnation point dropoff_age
		// Added as if to keep species pristine until the dropoff point
		if age_debt >= 1 {
			// Extreme penalty for a long period of stagnation (divide fitness by 100)
			org.Fitness = org.Fitness * 0.01
		}

		// Give a fitness boost up to some young age (niching)
		// The age_significance parameter is a system parameter
		// if it is 1, then young species get no fitness boost
		if s.Age <= 10 {
			org.Fitness = org.Fitness * conf.AgeSignificance
		}
		//Do not allow negative fitness
		if org.Fitness < 0.0 {
			org.Fitness = 0.0001
		}

		// Share fitness with the species
		org.Fitness = org.Fitness / float64(len(s.Organisms))
	}

	// Sort the population (most fit first) and mark for death those after : survival_thresh * pop_size
	sort.Sort(ByFitness(s.Organisms))

	// Update age_of_last_improvement here
	if s.Organisms[0].OriginalFitness > s.MaxFitnessEver {
		s.AgeOfLastImprovement = s.Age
		s.MaxFitnessEver = s.Organisms[0].OriginalFitness
	}

	// Decide how many get to reproduce based on survival_thresh * pop_size
	// Adding 1.0 ensures that at least one will survive
	num_parents := int(math.Floor(conf.SurvivalThresh * float64(len(s.Organisms)) + 1.0))

	//Mark for death those who are ranked too low to be parents
	s.Organisms[0].IsChampion = true // Mark the champ as such
	for c := num_parents; c < len(s.Organisms); c++ {
		s.Organisms[c].ToEliminate = true
	}
}

// Computes average species fitness
func (s *Species) computeAvgFitness() float64 {
	total := 0.0
	for _, o := range s.Organisms {
		total += o.Fitness
	}
	s.AvgFitness = total / float64(len(s.Organisms))
	return s.AvgFitness
}

// Computes maximal fitness of species
func (s *Species) computeMaxFitness() float64 {
	max := 0.0
	for _, o := range s.Organisms {
		if o.Fitness > max {
			max = o.Fitness
		}
	}
	s.MaxFitness = max
	return s.MaxFitness
}

// Compute the collective offspring the entire species (the sum of all organism's offspring) is assigned.
// The skim is fractional offspring left over from a previous species that was counted. These fractional parts are
// kept until they add up to 1.
// Returns fractional offspring left after computation (skim).
func (s *Species) countOffspring(skim float64) float64 {
	var e_o_intpart int  // The floor of an organism's expected offspring
	var e_o_fracpart float64 // Expected offspring fractional part
	var skim_intpart float64  // The whole offspring in the skim

	s.ExpectedOffspring = 0
	for _, o := range s.Organisms {
		e_o_intpart = int(math.Floor(o.ExpectedOffspring))
		e_o_fracpart = math.Mod(o.ExpectedOffspring, 1.0)

		s.ExpectedOffspring += e_o_intpart

		// Skim off the fractional offspring
		skim += e_o_fracpart

		if skim >= 1.0 {
			skim_intpart = math.Floor(skim)
			s.ExpectedOffspring += int(skim_intpart)
			skim -= skim_intpart
		}
	}
	return skim
}

// Compute generations since last improvement
func (s *Species) lastImproved() int {
	return s.Age - s.AgeOfLastImprovement
}

// Returns size of this Species, i.e. number of Organisms belonging to it
func (s *Species) size() int {
	return len(s.Organisms)
}

// Returns Organism - champion among others (best fitness)
func (s *Species) findChampion() *Organism {
	best_fitness := 0.0
	var champion *Organism
	for _, o := range s.Organisms {
		if o.Fitness > best_fitness {
			best_fitness = o.Fitness
			champion = o
		}
	}
	return champion
}

//Perform mating and mutation to form next generation
func (s *Species) reproduce(generation int, pop *Population, sorted_species *Species, conf *neat.Neat) (bool, error) {
	//Check for a mistake
	if s.ExpectedOffspring > 0 && len(s.Organisms == 0) {
		return false, errors.New("ATTEMPT TO REPRODUCE OUT OF EMPTY SPECIES")
	}

	poolsize := len(s.Organisms)  //The number of Organisms in the old generation
	// The champion of the 'this' specie is the first element of the specie;
	thechamp := s.Organisms[0]

	// TODO check if we really need this
	var net_analogue *network.Network  // For adding link to test for reccurrency

	// Parent Organisms and new Organism
	var mom, dad, baby *Organism

	// For holding baby's genes
	var new_genome *Genome

	// For mating outside the Species
	var randspecies *Species

	// The weight mutation power is species specific depending on its age
	mut_power := conf.WeightMutPower
	// Flag the preservation of the champion
	champ_done := false

	var outside, mut_struct_baby, mate_baby bool

	// Create the designated number of offspring for the Species one at a time
	for count := 0; count < s.ExpectedOffspring; count++ {
		outside, mut_struct_baby, mate_baby = false, false, false

		// Debug Trap
		if s.ExpectedOffspring > conf.PopSize {
			fmt.Printf("ALERT: EXPECTED OFFSPRING = %d", s.ExpectedOffspring)
		}

		if thechamp.SuperChampOffspring > 0 {
			// If we have a super_champ (Population champion), finish off some special clones
			mom = thechamp;
			new_genome = mom.GNome.duplicate(count)

			// Most superchamp offspring will have their connection weights mutated only
			// The last offspring will be an exact duplicate of this super_champ
			// Note: Superchamp offspring only occur with stolen babies!
			//      Settings used for published experiments did not use this
			if thechamp.SuperChampOffspring > 1 {
				if rand.Float64() < 0.8 || conf.MutateAddLinkProb == 0.0 {
					// Make sure no links get added when the system has link adding disabled
					new_genome.mutateLinkWeights(mut_power, 1.0, GAUSSIAN)
				} else {
					//Sometimes we add a link to a superchamp
					net_analogue = new_genome.genesis(generation)
					new_genome.mutateAddLink(pop, conf.NewLinkTries)
					mut_struct_baby = true;
				}
			}

			baby = NewOrganism(0.0, new_genome, generation)

			if thechamp.SuperChampOffspring == 1 {
				if thechamp.IsPopulationChampion {
					baby.IsPopulationChampionChild = true
					baby.highestFitness = mom.OriginalFitness
				}
			}

			thechamp.SuperChampOffspring--
		} else if !champ_done && s.ExpectedOffspring > 5 {
			// If we have a Species champion, just clone it
			mom = thechamp // Mom is the champ
			new_genome = mom.GNome.duplicate(count)
			baby = NewOrganism(0.0, new_genome, generation) // Baby is just like mommy
			champ_done = true
		} else if rand.Float64() < conf.MutateOnlyProb || poolsize == 1 {
			// Apply mutations
			orgnum := rand.Int31n(poolsize) // select random mom
			mom = s.Organisms[orgnum]
			new_genome = mom.GNome.duplicate(count)

			// Do the mutation depending on probabilities of various mutations
			if rand.Float64() < conf.MutateAddNodeProb {
				// Mutate add node
				new_genome.mutateAddNode(pop)
				mut_struct_baby = true
			} else if rand.Float64() < conf.MutateAddLinkProb {
				// Mutate add link
				net_analogue = new_genome.genesis(generation)
				new_genome.mutateAddLink(pop, conf.NewLinkTries)
				mut_struct_baby = true
			} else {
				//If we didn't do a structural mutation, we do the other kinds
				new_genome.mutateAllNonstructural(conf)
			}

			baby = NewOrganism(0.0, new_genome, generation);
		} else {
			// Otherwise we should mate
			orgnum := rand.Int31n(poolsize) // select random mom
			mom = s.Organisms[orgnum]

			// Choose random dad
			if rand.Float64() > conf.InterspeciesMateRate {
				// Mate within Species
				orgnum = rand.Int31n(poolsize)
				dad = s.Organisms[orgnum]
			} else {
				// Mate outside Species
				randspecies = s

				// Select a random species
				giveup := 0
				for ;randspecies == s && giveup < 5; {

					//Choose a random species tending towards better species
					randmult := gaussian.StdGaussian() / 4.0
					if randmult > 1.0 { randmult = 1.0 }
					// This tends to select better species
					randspeciesnum := int(math.Floor(randmult * (float64(len(sorted_species)) - 1.0) + 0.5))
					randspecies = sorted_species[randspeciesnum]

					giveup++
				}
				dad = randspecies.Organisms[0]
			}

			// Perform mating based on probabilities of different mating types
			if rand.Float64() < conf.MateMultipointProb {
				// mate multipoint baby
				new_genome.mateMultipoint(dad.GNome, count, mom.OriginalFitness, dad.OriginalFitness)
			} else if rand.Float64() < conf.MateMultipointAvgProb / (conf.MateMultipointAvgProb + conf.MateSinglepointProb) {
				// mate multipoint_avg baby
				new_genome.mateMultipointAvg(dad.GNome, count, mom.OriginalFitness, dad.OriginalFitness)
			} else {
				new_genome = mom.GNome.mateSinglepoint(dad.GNome, count)
			}

			mate_baby = true

			// Determine whether to mutate the baby's Genome
			// This is done randomly or if the mom and dad are the same organism
			if rand.Float64() > conf.MateOnlyProb ||
				dad.GNome.GenomeId == mom.GNome.GenomeId ||
				dad.GNome.compatibility(mom.GNome) == 0.0 {
				// Do the mutation depending on probabilities of  various mutations
				if rand.Float64() < conf.MutateAddNodeProb {
					// mutate_add_node
					new_genome.mutateAddNode(pop)
					mut_struct_baby = true
				} else if rand.Float64() < conf.MutateAddLinkProb {
					// mutate_add_link
					net_analogue = new_genome.genesis(generation)
					new_genome.mutateAddLink(pop, conf.NewLinkTries)
					mut_struct_baby = true
				} else {
					//Only do other mutations when not doing structural mutations
					new_genome.mutateAllNonstructural(conf)
				}

				//Create the baby
				baby = NewOrganism(0.0, new_genome, generation);
			} else {
				//Create the baby without mutating first
				baby = NewOrganism(0.0, new_genome, generation);
			}

			// Add the baby to its proper Species
			// If it doesn't fit a Species, create a new one
			baby.mutationStructBaby = mut_struct_baby
			baby.mateBaby = mate_baby

			if pop.species == nil || len(pop.species) == 0 {
				// Create the first species
				createFirstSpecies(pop, baby)
			} else {
				found := false
				for i := 0; i < len(pop.species) && !found; i++ {
					// point _species
					_specie := pop.species[i]
					if len(_specie.Organisms) > 0 {
						// point to first organism of this _specie
						compare_org := _specie.Organisms[0]
						// compare baby organism with first organism in current specie
						curr_compat := baby.GNome.compatibility(compare_org.GNome)

						if curr_compat < conf.CompatThreshold {
							// Found compatible species, so add this baby to it
							_specie.addOrganism(baby);
							// update in baby pointer to its species
							baby.SpeciesOf = _specie
							// force exit from this block ...
							found = true;
						}
					}
				}

				// If we didn't find a match, create a new species
				if !found {
					createFirstSpecies(pop, baby)
				}

			} //end else
		}

	} // end for count := 0
	return true;
}

func createFirstSpecies(pop *Population, baby *Organism) {
	pop.lastSpecies++
	new_species := NewSpeciesNovel(pop.lastSpecies, true)
	pop.species = append(pop.species, new_species)
	new_species.addOrganism(baby) // Add the baby
	baby.SpeciesOf = new_species //Point baby to its species
}

func (s *Species) String() string {
	str := fmt.Sprintf("Species #%d, age=%d, avg_fitness=%.3f, max_fitness=%.3f, max_fitness_ever=%.3f, expected_offspring=%d, age_of_last_improvement=%d\n",
		s.Id, s.Age, s.AvgFitness, s.MaxFitness, s.MaxFitnessEver, s.ExpectedOffspring, s.AgeOfLastImprovement)
	str += fmt.Sprintf("Has %d Organisms\n", len(s.Organisms))
	for _, o := range s.Organisms {
		str += fmt.Sprintf("%s\n", o)
	}
	return str
}